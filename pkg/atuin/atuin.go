// Package atuin reads local Atuin shell history without modifying its database.
package atuin

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jovandeginste/personalactivity/pkg/activity"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Source struct{ path string }

func New(path string) Source { return Source{path: path} }
func (Source) Name() string  { return "atuin" }

type history struct {
	Timestamp int64  `gorm:"column:timestamp"`
	Command   string `gorm:"column:command"`
	Cwd       string `gorm:"column:cwd"`
	Hostname  string `gorm:"column:hostname"`
	Exit      int    `gorm:"column:exit"`
	Duration  int64  `gorm:"column:duration"`
}

func (history) TableName() string { return "history" }

func (s Source) Collect(ctx context.Context, dateRange activity.Range) ([]activity.Event, error) {
	if _, err := os.Stat(s.path); err != nil {
		slog.Warn("skipping source", "source", s.Name(), "error", err)
		return []activity.Event{}, nil
	}
	slog.Info("collecting activity", "source", s.Name())
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("determine current hostname: %w", err)
	}
	dsn := (&url.URL{Scheme: "file", Path: s.path, RawQuery: "mode=ro"}).String()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(os.Stderr, "", log.LstdFlags), gormlogger.Config{LogLevel: gormlogger.Error}),
	})
	if err != nil {
		return nil, fmt.Errorf("open read-only history database: %w", err)
	}
	var rows []history
	if err := db.WithContext(ctx).Where("hostname LIKE ?", hostname+":%").Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]activity.Event, 0, len(rows))
	for _, row := range rows {
		date := timestamp(row.Timestamp)
		if !dateRange.Contains(date) {
			continue
		}
		cwd := homePath(row.Cwd)
		host, _, _ := strings.Cut(row.Hostname, ":")
		events = append(events, activity.Event{
			Date: date, Source: s.Name(), Topic: cwd,
			Summary: row.Command,
			Attributes: map[string]string{
				"cwd": cwd, "exit": strconv.Itoa(row.Exit), "duration": strconv.FormatInt(row.Duration, 10), "host": host,
			},
		})
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(rows), "filtered", len(events))
	return events, nil
}

func timestamp(value int64) time.Time {
	if value > 1_000_000_000_000 {
		return time.Unix(0, value)
	}
	return time.Unix(value, 0)
}

func homePath(path string) string {
	currentUser, err := user.Current()
	if err != nil {
		return path
	}
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		if segment == currentUser.Username {
			if rest := strings.Join(segments[index+1:], "/"); rest != "" {
				return "~/" + rest
			}
			return "~"
		}
	}
	return path
}
