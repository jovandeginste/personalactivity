// Package opencode collects user prompts from the local OpenCode database.
package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jovandeginste/personalactivity/pkg/activity"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Source struct{ path string }

func New(path string) Source { return Source{path: path} }
func (Source) Name() string  { return "opencode" }

type message struct {
	SessionID   string `gorm:"column:session_id"`
	TimeCreated int64  `gorm:"column:time_created"`
	Data        string `gorm:"column:data"`
	Directory   string `gorm:"column:directory"`
	Title       string `gorm:"column:title"`
	Agent       string `gorm:"column:agent"`
	Model       string `gorm:"column:model"`
}

type messageData struct {
	Text string `json:"text"`
}

func (s Source) Collect(ctx context.Context) ([]activity.Event, error) {
	if _, err := os.Stat(s.path); err != nil {
		slog.Warn("skipping source", "source", s.Name(), "error", err)
		return []activity.Event{}, nil
	}
	slog.Info("collecting activity", "source", s.Name())
	dsn := (&url.URL{Scheme: "file", Path: s.path, RawQuery: "mode=ro"}).String()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(os.Stderr, "", log.LstdFlags), gormlogger.Config{LogLevel: gormlogger.Error}),
	})
	if err != nil {
		return nil, fmt.Errorf("open read-only OpenCode database: %w", err)
	}
	var messages []message
	if err := db.WithContext(ctx).Raw(`
		SELECT m.session_id, m.time_created, m.data, s.directory, s.title, s.agent, s.model
		FROM session_message AS m
		JOIN session_v2 AS s ON s.id = m.session_id
		WHERE m.type = 'user'
	`).Scan(&messages).Error; err != nil {
		return nil, err
	}

	events := make([]activity.Event, 0, len(messages))
	for _, message := range messages {
		event, ok, err := s.event(message)
		if err != nil {
			return nil, err
		}
		if ok {
			events = append(events, event)
		}
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(events))
	return events, nil
}

func (s Source) event(message message) (activity.Event, bool, error) {
	var data messageData
	if err := json.Unmarshal([]byte(message.Data), &data); err != nil {
		return activity.Event{}, false, fmt.Errorf("decode OpenCode message %s: %w", message.SessionID, err)
	}
	text := strings.TrimSpace(data.Text)
	if text == "" {
		return activity.Event{}, false, nil
	}
	return activity.Event{
		Date:    time.UnixMilli(message.TimeCreated),
		Source:  s.Name(),
		Topic:   homePath(message.Directory),
		Summary: text,
		Attributes: map[string]string{
			"session": message.SessionID,
			"title":   message.Title,
			"agent":   message.Agent,
			"model":   message.Model,
		},
	}, true, nil
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
