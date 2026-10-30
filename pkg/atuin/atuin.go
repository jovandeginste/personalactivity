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
	Exit      int    `gorm:"column:exit"`
	Duration  int64  `gorm:"column:duration"`
}

func (history) TableName() string { return "history" }

func (s Source) Collect(ctx context.Context) ([]activity.Event, error) {
	slog.Info("collecting activity", "source", s.Name())
	dsn := (&url.URL{Scheme: "file", Path: s.path, RawQuery: "mode=ro"}).String()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(os.Stderr, "", log.LstdFlags), gormlogger.Config{LogLevel: gormlogger.Error}),
	})
	if err != nil {
		return nil, fmt.Errorf("open read-only history database: %w", err)
	}
	var rows []history
	if err := db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]activity.Event, 0, len(rows))
	for _, row := range rows {
		cwd := homePath(row.Cwd)
		events = append(events, activity.Event{
			Date: timestamp(row.Timestamp), Source: s.Name(), Topic: cwd,
			Summary: row.Command,
			Attributes: map[string]string{
				"cwd": cwd, "exit": strconv.Itoa(row.Exit), "duration": strconv.FormatInt(row.Duration, 10),
			},
		})
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(events))
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
