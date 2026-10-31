// Package firefox collects visited URLs from the default local Firefox profile in read-only mode.
package firefox

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jovandeginste/personalactivity/pkg/activity"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const compactWindow = 10 * time.Minute

type Source struct{ path string }

func New(path string) Source { return Source{path: path} }
func (Source) Name() string  { return "firefox" }

type visit struct {
	VisitDate   int64  `gorm:"column:visit_date"`
	VisitType   int    `gorm:"column:visit_type"`
	URL         string `gorm:"column:url"`
	Title       string `gorm:"column:title"`
	Description string `gorm:"column:description"`
}

func (s Source) Collect(ctx context.Context) ([]activity.Event, error) {
	dbPath, err := placesPath(s.path)
	if err != nil {
		slog.Warn("skipping source", "source", s.Name(), "error", err)
		return []activity.Event{}, nil
	}
	slog.Info("collecting activity", "source", s.Name())

	dsn := (&url.URL{Scheme: "file", Path: dbPath, RawQuery: "mode=ro&immutable=1"}).String()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(os.Stderr, "", log.LstdFlags), gormlogger.Config{LogLevel: gormlogger.Error}),
	})
	if err != nil {
		return nil, fmt.Errorf("open read-only Firefox database: %w", err)
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	var rows []visit
	if err := db.WithContext(ctx).Raw(`
		SELECT
			v.visit_date,
			COALESCE(v.visit_type, 0) AS visit_type,
			p.url,
			COALESCE(p.title, '') AS title,
			COALESCE(p.description, '') AS description
		FROM moz_historyvisits AS v
		JOIN moz_places AS p ON p.id = v.place_id
		WHERE v.visit_date IS NOT NULL
		  AND COALESCE(p.hidden, 0) = 0
		  AND NOT EXISTS (
			SELECT 1 FROM moz_historyvisits AS r
			WHERE r.from_visit = v.id AND r.visit_type IN (5, 6)
		  )
		ORDER BY v.visit_date ASC
	`).Scan(&rows).Error; err != nil {
		return nil, err
	}

	events := make([]activity.Event, 0, len(rows))
	for _, row := range rows {
		date := time.UnixMicro(row.VisitDate)
		rawURL := cleanURL(row.URL)
		title := strings.TrimSpace(row.Title)
		description := strings.TrimSpace(row.Description)
		if idx := findRecentEvent(events, rawURL, title, date); idx >= 0 {
			match := events[idx]
			count := 1
			if prev, err := strconv.Atoi(match.Attributes["count"]); err == nil {
				count = prev
			} else {
				match.Attributes["first_visit"] = match.Date.Format(time.RFC3339)
			}
			match.Date = date
			match.Summary = formatSummary(title, rawURL)
			match.Attributes["url"] = rawURL
			match.Attributes["visit_type"] = visitTypeName(row.VisitType)
			if description != "" {
				match.Attributes["description"] = description
			}
			match.Attributes["count"] = strconv.Itoa(count + 1)
			match.Attributes["last_visit"] = date.Format(time.RFC3339)
			copy(events[idx:], events[idx+1:])
			events[len(events)-1] = match
			continue
		}
		events = append(events, activity.Event{
			Date:    date,
			Source:  s.Name(),
			Topic:   hostTopic(rawURL),
			Summary: formatSummary(title, rawURL),
			Attributes: map[string]string{
				"url":         rawURL,
				"title":       title,
				"description": description,
				"visit_type":  visitTypeName(row.VisitType),
			},
		})
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(events))
	return events, nil
}

func visitTypeName(visitType int) string {
	switch visitType {
	case 1:
		return "link"
	case 2:
		return "typed"
	case 3:
		return "bookmark"
	case 4:
		return "embed"
	case 5:
		return "redirect_permanent"
	case 6:
		return "redirect_temporary"
	case 7:
		return "download"
	case 8:
		return "framed_link"
	case 9:
		return "reload"
	default:
		return strconv.Itoa(visitType)
	}
}

func formatSummary(title, rawURL string) string {
	if title != "" {
		return title + " (" + rawURL + ")"
	}
	return rawURL
}

func findRecentEvent(events []activity.Event, rawURL, title string, date time.Time) int {
	for i := len(events) - 1; i >= 0; i-- {
		if date.Sub(events[i].Date) > compactWindow {
			break
		}
		if events[i].Attributes["title"] == title && strings.HasPrefix(rawURL, events[i].Attributes["url"]) {
			return i
		}
	}
	return -1
}

func hostTopic(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return rawURL
	}
	return parsed.Host
}

var ignoredQueryParams = map[string]bool{
	"_ga":                      true,
	"_gl":                      true,
	"aqs":                      true,
	"bih":                      true,
	"biw":                      true,
	"channel":                  true,
	"cid":                      true,
	"client":                   true,
	"dclid":                    true,
	"ei":                       true,
	"fbclid":                   true,
	"gbraid":                   true,
	"gclid":                    true,
	"gi":                       true,
	"gs_lcrp":                  true,
	"gs_lp":                    true,
	"hs":                       true,
	"iflsig":                   true,
	"igshid":                   true,
	"mc_cid":                   true,
	"mc_eid":                   true,
	"msclkid":                  true,
	"notification_referrer_id": true,
	"oq":                       true,
	"ref":                      true,
	"ref_src":                  true,
	"ref_url":                  true,
	"referrer":                 true,
	"sca_esv":                  true,
	"sclient":                  true,
	"sourceid":                 true,
	"sxsrf":                    true,
	"twclid":                   true,
	"uact":                     true,
	"ved":                      true,
	"wbraid":                   true,
}

func cleanURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.RawQuery == "" {
		return rawURL
	}
	var kept []string
	for part := range strings.SplitSeq(parsed.RawQuery, "&") {
		if part == "" {
			continue
		}
		key, _, _ := strings.Cut(part, "=")
		key = strings.ToLower(key)
		if ignoredQueryParams[key] || strings.HasPrefix(key, "utm_") || strings.HasPrefix(key, "__cf_chl_") {
			continue
		}
		kept = append(kept, part)
	}
	parsed.RawQuery = strings.Join(kept, "&")
	return parsed.String()
}

func placesPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return path, nil
	}
	direct := filepath.Join(path, "places.sqlite")
	if _, err := os.Stat(direct); err == nil {
		return direct, nil
	}
	profileDir, err := defaultProfileDir(path)
	if err != nil {
		return "", err
	}
	dbPath := filepath.Join(profileDir, "places.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		return "", err
	}
	return dbPath, nil
}

type profileEntry struct {
	path       string
	isRelative bool
	isDefault  bool
}

func defaultProfileDir(firefoxDir string) (string, error) {
	file, err := os.Open(filepath.Join(firefoxDir, "profiles.ini"))
	if err != nil {
		return "", fmt.Errorf("open Firefox profiles.ini: %w", err)
	}
	defer file.Close()

	var profiles []profileEntry
	var current *profileEntry
	var installDefault string
	inInstall := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if current != nil {
				profiles = append(profiles, *current)
				current = nil
			}
			section := line[1 : len(line)-1]
			inInstall = strings.HasPrefix(section, "Install")
			if strings.HasPrefix(section, "Profile") {
				current = &profileEntry{isRelative: true}
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if inInstall && key == "Default" && installDefault == "" {
			installDefault = value
		}
		if current == nil {
			continue
		}
		switch key {
		case "Path":
			current.path = value
		case "IsRelative":
			current.isRelative = value != "0"
		case "Default":
			current.isDefault = value == "1"
		}
	}
	if current != nil {
		profiles = append(profiles, *current)
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	for _, p := range profiles {
		if p.isDefault && p.path != "" {
			return resolveProfilePath(firefoxDir, p), nil
		}
	}
	if installDefault != "" {
		return filepath.Join(firefoxDir, installDefault), nil
	}
	for _, p := range profiles {
		if p.path != "" {
			return resolveProfilePath(firefoxDir, p), nil
		}
	}
	return "", fmt.Errorf("no default Firefox profile found in %s", firefoxDir)
}

func resolveProfilePath(firefoxDir string, p profileEntry) string {
	if p.isRelative {
		return filepath.Join(firefoxDir, p.path)
	}
	return p.path
}
