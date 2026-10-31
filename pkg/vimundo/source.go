package vimundo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jovandeginste/personalactivity/pkg/activity"
)

const compactWindow = time.Minute

type Source struct{ directory string }

func New(directory string) Source { return Source{directory: directory} }
func (Source) Name() string       { return "vimundo" }

func (s Source) Collect(_ context.Context, dateRange activity.Range) ([]activity.Event, error) {
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		slog.Warn("skipping source", "source", s.Name(), "error", err)
		return []activity.Event{}, nil
	}
	slog.Info("collecting activity", "source", s.Name())
	var total int
	var events []activity.Event
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		undo, err := ReadFile(filepath.Join(s.directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		compacted := compactHeaders(undo.Headers)
		total += len(compacted)
		for _, item := range compacted {
			if !dateRange.Contains(item.header.Time) {
				continue
			}
			events = append(events, activity.Event{
				Date:    item.header.Time,
				Source:  s.Name(),
				Topic:   homePath(originalPath(entry.Name())),
				Summary: formatSummary(item.lines),
				Attributes: map[string]string{
					"sequence": strconv.FormatUint(uint64(item.header.Sequence), 10),
					"entries":  strconv.Itoa(item.entries),
				},
			})
		}
	}
	slog.Info("collected activity", "source", s.Name(), "events", total, "filtered", len(events))
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

type compactedHeader struct {
	header  Header
	line    uint32
	entries int
	lines   []string
}

func compactHeaders(headers []Header) []compactedHeader {
	sorted := append([]Header(nil), headers...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Time.Equal(sorted[j].Time) {
			return sorted[i].Sequence < sorted[j].Sequence
		}
		return sorted[i].Time.Before(sorted[j].Time)
	})

	var compacted []compactedHeader
	for _, h := range sorted {
		line := headerLine(h)
		lines := headerLines(h)
		if len(compacted) > 0 {
			last := &compacted[len(compacted)-1]
			if last.line == line && h.Time.Sub(last.header.Time) <= compactWindow {
				if len(lines) > 0 {
					if isLineDelete(h) && h.Time.Equal(last.header.Time) {
						last.lines = append(last.lines, lines...)
					} else {
						last.lines = lines
					}
				}
				last.header = h
				last.entries += len(h.Entries)
				continue
			}
		}
		compacted = append(compacted, compactedHeader{
			header:  h,
			line:    line,
			entries: len(h.Entries),
			lines:   lines,
		})
	}
	return compacted
}

func headerLine(h Header) uint32 {
	if len(h.Entries) > 0 {
		return h.Entries[0].Top
	}
	return h.Cursor.Line
}

func isLineDelete(h Header) bool {
	return len(h.Entries) == 1 && h.Entries[0].Bottom == h.Entries[0].Top+1
}

func headerLines(h Header) []string {
	var lines []string
	for _, entry := range h.Entries {
		for _, line := range entry.Lines {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				lines = append(lines, trimmed)
			}
		}
	}
	return lines
}

func formatSummary(lines []string) string {
	text := strings.TrimSpace(strings.Join(lines, " "))
	if text == "" {
		return "Changed no lines"
	}
	const limit = 1000
	if len(text) > limit {
		return text[:limit-1] + "…"
	}
	return text
}
