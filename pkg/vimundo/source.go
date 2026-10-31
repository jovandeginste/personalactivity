package vimundo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jovandeginste/personalactivity/pkg/activity"
)

type Source struct{ directory string }

func New(directory string) Source { return Source{directory: directory} }
func (Source) Name() string       { return "vimundo" }

func (s Source) Collect(_ context.Context) ([]activity.Event, error) {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		slog.Warn("skipping source", "source", s.Name(), "error", err)
		return []activity.Event{}, nil
	}
	slog.Info("collecting activity", "source", s.Name())
	var events []activity.Event
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		undo, err := ReadFile(filepath.Join(s.directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		for _, header := range undo.Headers {
			events = append(events, activity.Event{
				Date:    header.Time,
				Source:  s.Name(),
				Topic:   homePath(originalPath(entry.Name())),
				Summary: summary(header),
				Attributes: map[string]string{
					"sequence": strconv.FormatUint(uint64(header.Sequence), 10),
					"entries":  strconv.Itoa(len(header.Entries)),
				},
			})
		}
	}
	slog.Info("collected activity", "source", s.Name(), "events", len(events))
	return events, nil
}

func originalPath(name string) string { return strings.ReplaceAll(name, "%", "/") }

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

func summary(header Header) string {
	var lines []string
	for _, entry := range header.Entries {
		lines = append(lines, entry.Lines...)
	}
	text := strings.TrimSpace(strings.Join(lines, " "))
	if text == "" {
		return "Changed no lines"
	}
	const limit = 200
	if len(text) > limit {
		return text[:limit-1] + "…"
	}
	return text
}
