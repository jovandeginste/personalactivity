// Package thunderbird collects sent emails and calendar events from local Thunderbird databases in read-only mode.
package thunderbird

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jovandeginste/personalactivity/pkg/activity"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Source struct{ path string }

func New(path string) Source { return Source{path: path} }
func (Source) Name() string  { return "thunderbird" }

type sentMessage struct {
	Date       int64  `gorm:"column:date"`
	MessageID  string `gorm:"column:headerMessageID"`
	FolderURI  string `gorm:"column:folderURI"`
	Subject    string `gorm:"column:subject"`
	Body       string `gorm:"column:body"`
	Sender     string `gorm:"column:sender"`
	To         string `gorm:"column:to_recipients"`
	Cc         string `gorm:"column:cc_recipients"`
	Bcc        string `gorm:"column:bcc_recipients"`
	Recipients string `gorm:"column:recipients"`
}

type calendarEvent struct {
	CalID       string `gorm:"column:cal_id"`
	ID          string `gorm:"column:id"`
	Title       string `gorm:"column:title"`
	Status      string `gorm:"column:status"`
	Privacy     string `gorm:"column:privacy"`
	EventStart  int64  `gorm:"column:event_start"`
	EventEnd    int64  `gorm:"column:event_end"`
	StartTZ     string `gorm:"column:start_tz"`
	EndTZ       string `gorm:"column:end_tz"`
	Location    string `gorm:"column:location"`
	Description string `gorm:"column:description"`
	URL         string `gorm:"column:url"`
	Attendees   string `gorm:"column:attendees"`
}

func (s Source) Collect(ctx context.Context) ([]activity.Event, error) {
	messagePaths, calendarPaths, err := databasePaths(s.path)
	if err != nil {
		slog.Warn("skipping source", "source", s.Name(), "error", err)
		return []activity.Event{}, nil
	}
	slog.Info("collecting activity", "source", s.Name())

	var events []activity.Event
	for _, dbPath := range messagePaths {
		profileEvents, err := s.collectMessagesDB(ctx, dbPath)
		if err != nil {
			return nil, err
		}
		events = append(events, profileEvents...)
	}
	for _, dbPath := range calendarPaths {
		profileEvents, err := s.collectCalendarDB(ctx, dbPath)
		if err != nil {
			return nil, err
		}
		events = append(events, profileEvents...)
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(events))
	return events, nil
}

func openReadOnlyDB(dbPath string) (*gorm.DB, error) {
	dsn := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro&immutable=1"}).String()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(os.Stderr, "", log.LstdFlags), gormlogger.Config{LogLevel: gormlogger.Error}),
	})
	if err != nil {
		return nil, fmt.Errorf("open read-only Thunderbird database: %w", err)
	}
	return db, nil
}

const sentMessagesQuery = `
	SELECT
		m.date,
		m.headerMessageID,
		f.folderURI,
		t.c1subject AS subject,
		COALESCE(t.c0body, '') AS body,
		COALESCE((SELECT value FROM identities WHERE id = json_extract(m.jsonAttributes, '$.43')), '') AS sender,
		COALESCE((SELECT group_concat(i.value, ', ') FROM json_each(m.jsonAttributes, '$.44') j JOIN identities i ON i.id = j.value), '') AS to_recipients,
		COALESCE((SELECT group_concat(i.value, ', ') FROM json_each(m.jsonAttributes, '$.45') j JOIN identities i ON i.id = j.value), '') AS cc_recipients,
		COALESCE((SELECT group_concat(i.value, ', ') FROM json_each(m.jsonAttributes, '$.46') j JOIN identities i ON i.id = j.value), '') AS bcc_recipients,
		COALESCE((SELECT group_concat(i.value, ', ') FROM json_each(m.jsonAttributes, '$.53') j JOIN identities i ON i.id = j.value), '') AS recipients
	FROM messages AS m
	JOIN folderLocations AS f ON f.id = m.folderID
	JOIN messagesText_content AS t ON t.docid = m.id
	WHERE (f.indexingPriority = 60 OR lower(f.name) LIKE 'sent%' OR lower(f.name) LIKE 'verzonden%')
	  AND m.deleted = 0
	GROUP BY CASE WHEN m.headerMessageID != '' THEN m.headerMessageID ELSE m.id END
`

// ponytail: queries stored cal_events rows directly; add RRULE expansion from cal_recurrence if unexpanded recurring instances are needed.
const calendarEventsQuery = `
	SELECT
		e.cal_id,
		e.id,
		COALESCE(e.title, '') AS title,
		COALESCE(e.ical_status, '') AS status,
		COALESCE(e.privacy, '') AS privacy,
		e.event_start,
		COALESCE(e.event_end, 0) AS event_end,
		COALESCE(e.event_start_tz, '') AS start_tz,
		COALESCE(e.event_end_tz, '') AS end_tz,
		COALESCE((SELECT CAST(p.value AS TEXT) FROM cal_properties p WHERE p.cal_id = e.cal_id AND p.item_id = e.id AND p.recurrence_id IS e.recurrence_id AND p.key = 'LOCATION' LIMIT 1), '') AS location,
		COALESCE((SELECT CAST(p.value AS TEXT) FROM cal_properties p WHERE p.cal_id = e.cal_id AND p.item_id = e.id AND p.recurrence_id IS e.recurrence_id AND p.key = 'DESCRIPTION' LIMIT 1), '') AS description,
		COALESCE((SELECT CAST(p.value AS TEXT) FROM cal_properties p WHERE p.cal_id = e.cal_id AND p.item_id = e.id AND p.recurrence_id IS e.recurrence_id AND p.key = 'URL' LIMIT 1), '') AS url,
		COALESCE((SELECT group_concat(a.icalString, char(10)) FROM cal_attendees a WHERE a.cal_id = e.cal_id AND a.item_id = e.id AND a.recurrence_id IS e.recurrence_id), '') AS attendees
	FROM cal_events AS e
	WHERE e.event_start IS NOT NULL
	  AND COALESCE(e.ical_status, '') != 'CANCELLED'
	  AND NOT EXISTS (
		SELECT 1 FROM cal_properties p
		WHERE p.cal_id = e.cal_id AND p.item_id = e.id AND p.recurrence_id IS e.recurrence_id AND p.key = 'X-MOZ-FAKED-MASTER'
	  )
	  AND NOT (
		e.recurrence_id IS NULL AND EXISTS (
			SELECT 1 FROM cal_events e2
			WHERE e2.cal_id = e.cal_id AND e2.id = e.id AND e2.recurrence_id = e.event_start
		)
	  )
`

func (s Source) collectMessagesDB(ctx context.Context, dbPath string) ([]activity.Event, error) {
	db, err := openReadOnlyDB(dbPath)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	var rows []sentMessage
	if err := db.WithContext(ctx).Raw(sentMessagesQuery).Scan(&rows).Error; err != nil {
		return nil, err
	}

	events := make([]activity.Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, s.messageEvent(row))
	}
	return events, nil
}

func (s Source) messageEvent(row sentMessage) activity.Event {
	destinations := row.Recipients
	if destinations == "" {
		destinations = row.To
	}
	topic := destinations
	if row.Sender != "" && destinations != "" {
		topic = row.Sender + " -> " + destinations
	} else if row.Sender != "" {
		topic = row.Sender
	}
	subject := strings.TrimSpace(row.Subject)
	body := cleanBody(row.Body)
	bodyLine := strings.Join(strings.Fields(body), " ")
	summary := subject
	if bodyLine != "" {
		if summary != "" {
			summary += ": " + bodyLine
		} else {
			summary = bodyLine
		}
	}
	return activity.Event{
		Date:    time.UnixMicro(row.Date),
		Source:  s.Name(),
		Topic:   topic,
		Summary: summary,
		Attributes: map[string]string{
			"from":       row.Sender,
			"to":         row.To,
			"cc":         row.Cc,
			"bcc":        row.Bcc,
			"subject":    subject,
			"body":       body,
			"folder":     row.FolderURI,
			"message_id": row.MessageID,
		},
	}
}

func (s Source) collectCalendarDB(ctx context.Context, dbPath string) ([]activity.Event, error) {
	db, err := openReadOnlyDB(dbPath)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	var rows []calendarEvent
	if err := db.WithContext(ctx).Raw(calendarEventsQuery).Scan(&rows).Error; err != nil {
		return nil, err
	}

	calNames := calendarNames(dbPath)
	events := make([]activity.Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, s.calendarActivityEvent(row, calNames[row.CalID]))
	}
	return events, nil
}

func (s Source) calendarActivityEvent(row calendarEvent, calendar string) activity.Event {
	if calendar == "" {
		calendar = row.CalID
	}
	title := strings.TrimSpace(row.Title)
	location := strings.Join(strings.Fields(row.Location), " ")
	description := cleanBody(row.Description)
	summary := formatCalendarSummary(title, location, strings.Join(strings.Fields(description), " "))

	startTime := time.UnixMicro(row.EventStart)
	endStr := ""
	if row.EventEnd > 0 {
		endStr = time.UnixMicro(row.EventEnd).Format(time.RFC3339)
	}
	organizer, attendees := parseAttendees(row.Attendees)

	return activity.Event{
		Date:    startTime,
		Source:  s.Name(),
		Topic:   calendar,
		Summary: summary,
		Attributes: map[string]string{
			"calendar_id": row.CalID,
			"calendar":    calendar,
			"event_id":    row.ID,
			"title":       title,
			"start":       startTime.Format(time.RFC3339),
			"end":         endStr,
			"start_tz":    row.StartTZ,
			"end_tz":      row.EndTZ,
			"status":      row.Status,
			"privacy":     row.Privacy,
			"location":    location,
			"description": description,
			"url":         strings.TrimSpace(row.URL),
			"organizer":   organizer,
			"attendees":   attendees,
		},
	}
}

func formatCalendarSummary(title, location, descLine string) string {
	summary := title
	if location != "" {
		if summary != "" {
			summary += " (" + location + ")"
		} else {
			summary = location
		}
	}
	if descLine != "" {
		if summary != "" {
			summary += ": " + descLine
		} else {
			summary = descLine
		}
	}
	return summary
}

func parseAttendees(raw string) (string, string) {
	var organizers, attendees []string
	for line := range strings.SplitSeq(raw, "\n") {
		kind, formatted := parseAttendeeLine(line)
		switch kind {
		case "ORGANIZER":
			organizers = append(organizers, formatted)
		case "ATTENDEE":
			attendees = append(attendees, formatted)
		}
	}
	return strings.Join(organizers, ", "), strings.Join(attendees, ", ")
}

func parseAttendeeLine(line string) (string, string) {
	propPart, valPart, ok := strings.Cut(strings.TrimSpace(line), ":")
	if !ok {
		return "", ""
	}
	email := strings.TrimSpace(valPart)
	if len(email) >= 7 && strings.EqualFold(email[:7], "mailto:") {
		email = strings.TrimSpace(email[7:])
	}
	parts := strings.Split(propPart, ";")
	kind := strings.ToUpper(strings.TrimSpace(parts[0]))
	cn := extractCN(parts[1:])
	if cn != "" && !strings.EqualFold(cn, email) {
		if email != "" {
			return kind, cn + " <" + email + ">"
		}
		return kind, cn
	}
	return kind, email
}

func extractCN(params []string) string {
	for _, param := range params {
		if k, v, ok := strings.Cut(param, "="); ok && strings.EqualFold(strings.TrimSpace(k), "CN") {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

func calendarNames(cacheDBPath string) map[string]string {
	names := make(map[string]string)
	dir := filepath.Dir(cacheDBPath)
	if filepath.Base(dir) == "calendar-data" {
		dir = filepath.Dir(dir)
	}
	file, err := os.Open(filepath.Join(dir, "prefs.js"))
	if err != nil {
		return names
	}
	defer file.Close()

	const (
		prefix = `user_pref("calendar.registry.`
		middle = `.name", "`
		suffix = `");`
	)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, suffix) {
			continue
		}
		rest := line[len(prefix) : len(line)-len(suffix)]
		if calID, name, ok := strings.Cut(rest, middle); ok && !strings.Contains(calID, ".") {
			names[calID] = name
		}
	}
	return names
}

const maxBodyLength = 1000

var dataAttachmentPattern = regexp.MustCompile(`(!?\[[^\]]*\]\(data:[^)]*\)|\[data:[^\]]*\]|data:\S+)`)

func cleanBody(body string) string {
	if idx := strings.Index(body, "Content-Disposition: attachment"); idx >= 0 {
		body = body[:idx]
	}
	body = dataAttachmentPattern.ReplaceAllString(body, "")
	body = strings.TrimSpace(body)
	if runes := []rune(body); len(runes) > maxBodyLength {
		return string(runes[:maxBodyLength])
	}
	return body
}

func databasePaths(path string) ([]string, []string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		if filepath.Base(path) == "cache.sqlite" {
			return nil, []string{path}, nil
		}
		return []string{path}, nil, nil
	}

	var messagePaths, calendarPaths []string
	for _, pattern := range []string{
		filepath.Join(path, "global-messages-db.sqlite"),
		filepath.Join(path, "*", "global-messages-db.sqlite"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, nil, err
		}
		messagePaths = append(messagePaths, matches...)
	}
	for _, pattern := range []string{
		filepath.Join(path, "cache.sqlite"),
		filepath.Join(path, "calendar-data", "cache.sqlite"),
		filepath.Join(path, "*", "calendar-data", "cache.sqlite"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, nil, err
		}
		calendarPaths = append(calendarPaths, matches...)
	}
	if len(messagePaths) == 0 && len(calendarPaths) == 0 {
		return nil, nil, fmt.Errorf("no Thunderbird databases found in %s", path)
	}
	return messagePaths, calendarPaths, nil
}
