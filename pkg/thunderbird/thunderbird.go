// Package thunderbird collects sent emails from local Thunderbird global message databases in read-only mode.
package thunderbird

import (
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

func (s Source) Collect(ctx context.Context) ([]activity.Event, error) {
	slog.Info("collecting activity", "source", s.Name())
	dbPaths, err := databasePaths(s.path)
	if err != nil {
		return nil, err
	}

	var events []activity.Event
	for _, dbPath := range dbPaths {
		profileEvents, err := s.collectDB(ctx, dbPath)
		if err != nil {
			return nil, err
		}
		events = append(events, profileEvents...)
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(events))
	return events, nil
}

func (s Source) collectDB(ctx context.Context, dbPath string) ([]activity.Event, error) {
	dsn := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro&immutable=1"}).String()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(os.Stderr, "", log.LstdFlags), gormlogger.Config{LogLevel: gormlogger.Error}),
	})
	if err != nil {
		return nil, fmt.Errorf("open read-only Thunderbird database: %w", err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	var rows []sentMessage
	if err := db.WithContext(ctx).Raw(`
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
	`).Scan(&rows).Error; err != nil {
		return nil, err
	}

	events := make([]activity.Event, 0, len(rows))
	for _, row := range rows {
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
		events = append(events, activity.Event{
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
		})
	}
	return events, nil
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

func databasePaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	matches, err := filepath.Glob(filepath.Join(path, "*", "global-messages-db.sqlite"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no global-messages-db.sqlite found in %s", path)
	}
	return matches, nil
}
