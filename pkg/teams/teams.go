// Package teams collects sent and received messages from the local Teams SQLite archive.
package teams

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/jovandeginste/personalactivity/pkg/activity"
	"github.com/jovandeginste/teams-tui/pkg/archive"
)

const sourceName = "teams"

type Source struct {
	path string
}

func New(path string) Source { return Source{path: path} }
func (Source) Name() string  { return sourceName }

func (s Source) Collect(ctx context.Context, dateRange activity.Range) ([]activity.Event, error) {
	if _, err := os.Stat(s.path); err != nil {
		slog.Warn("skipping source", "source", s.Name(), "error", err)

		return []activity.Event{}, nil
	}

	slog.Info("collecting activity", "source", s.Name())

	store, err := archive.Open(s.path)
	if err != nil {
		return nil, err
	}

	var filter archive.Filter

	if dateRange.From != nil {
		filter.From = *dateRange.From
	}

	if dateRange.To != nil {
		filter.To = *dateRange.To
	}

	msgs, err := store.Messages(ctx, filter)
	if err != nil {
		return nil, err
	}

	events := make([]activity.Event, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Direction != "sent" && msg.Direction != "received" {
			continue
		}

		if !dateRange.Contains(msg.Timestamp) {
			continue
		}

		events = append(events, toEvent(msg))
	}

	slog.Info("collected activity", "source", s.Name(), "events", len(msgs), "filtered", len(events))

	return events, nil
}

func toEvent(msg archive.Message) activity.Event {
	topic := msg.ChannelName
	if topic == "" {
		topic = msg.ChannelID
	}

	contentLine := strings.Join(strings.Fields(msg.Content), " ")
	summary := contentLine

	if msg.AuthorName != "" && contentLine != "" {
		summary = msg.AuthorName + ": " + contentLine
	} else if msg.AuthorName != "" {
		summary = msg.AuthorName
	}

	threadID := ""
	if msg.ThreadID != nil {
		threadID = strconv.FormatInt(*msg.ThreadID, 10)
	}

	return activity.Event{
		Date:    msg.Timestamp,
		Source:  sourceName,
		Topic:   topic,
		Summary: summary,
		Attributes: map[string]string{
			"id":           strconv.FormatInt(msg.ID, 10),
			"thread_id":    threadID,
			"channel_id":   msg.ChannelID,
			"channel":      msg.ChannelName,
			"channel_type": msg.ChannelType,
			"direction":    msg.Direction,
			"author_id":    msg.AuthorID,
			"author":       msg.AuthorName,
			"content":      strings.TrimSpace(msg.Content),
			"reactions":    msg.Reactions,
		},
	}
}
