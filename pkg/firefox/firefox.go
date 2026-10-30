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
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jovandeginste/personalactivity/pkg/activity"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Source struct{ path string }

func New(path string) Source { return Source{path: path} }
func (Source) Name() string  { return "firefox" }

type visit struct {
	VisitDate   int64  `gorm:"column:visit_date"`
	URL         string `gorm:"column:url"`
	Title       string `gorm:"column:title"`
	Description string `gorm:"column:description"`
}

func (s Source) Collect(ctx context.Context) ([]activity.Event, error) {
	slog.Info("collecting activity", "source", s.Name())
	dbPath, err := placesPath(s.path)
	if err != nil {
		return nil, err
	}

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
			p.url,
			COALESCE(p.title, '') AS title,
			COALESCE(p.description, '') AS description
		FROM moz_historyvisits AS v
		JOIN moz_places AS p ON p.id = v.place_id
		WHERE v.visit_date IS NOT NULL
	`).Scan(&rows).Error; err != nil {
		return nil, err
	}

	events := make([]activity.Event, 0, len(rows))
	for _, row := range rows {
		title := strings.TrimSpace(row.Title)
		summary := row.URL
		if title != "" {
			summary = title + " (" + row.URL + ")"
		}
		events = append(events, activity.Event{
			Date:    time.UnixMicro(row.VisitDate),
			Source:  s.Name(),
			Topic:   hostTopic(row.URL),
			Summary: summary,
			Attributes: map[string]string{
				"url":         row.URL,
				"title":       title,
				"description": strings.TrimSpace(row.Description),
			},
		})
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(events))
	return events, nil
}

func hostTopic(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return rawURL
	}
	return parsed.Host
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
	return filepath.Join(profileDir, "places.sqlite"), nil
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
