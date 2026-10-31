// Package activity provides the shared model and collection logic for activity sources.
package activity

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Date       time.Time         `json:"date"`
	Source     string            `json:"source"`
	Topic      string            `json:"topic"`
	Summary    string            `json:"summary"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type Source interface {
	Name() string
	Collect(ctx context.Context, dateRange Range) ([]Event, error)
}

type Range struct {
	From *time.Time
	To   *time.Time // exclusive
}

func (r Range) Contains(t time.Time) bool {
	return (r.From == nil || !t.Before(*r.From)) && (r.To == nil || t.Before(*r.To))
}

func (r Range) Filter(events []Event) []Event {
	filtered := make([]Event, 0, len(events))
	for _, event := range events {
		if r.Contains(event.Date) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func ParseRange(from, to string) (Range, error) {
	now := time.Now()
	start, err := parseDate(from, false)
	if err != nil {
		return Range{}, fmt.Errorf("invalid --from: %w", err)
	}
	if start == nil {
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		start = &midnight
	}
	end, err := parseDate(to, true)
	if err != nil {
		return Range{}, fmt.Errorf("invalid --to: %w", err)
	}
	if end == nil {
		end = &now
	}
	if !start.Before(*end) {
		return Range{}, fmt.Errorf("--from must be before --to")
	}
	return Range{From: start, To: end}, nil
}

func parseDate(value string, endOfDate bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	if date, err := time.Parse("2006-01-02", value); err == nil {
		if endOfDate {
			date = date.AddDate(0, 0, 1)
		}
		return &date, nil
	}
	date, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("use YYYY-MM-DD or RFC3339")
	}
	return &date, nil
}

func Collect(ctx context.Context, sources []Source, selected []string, dateRange Range) ([]Event, error) {
	wanted := make(map[string]bool, len(selected))
	for _, name := range selected {
		wanted[strings.ToLower(name)] = true
	}

	var active []Source
	for _, source := range sources {
		if len(selected) > 0 && !wanted[strings.ToLower(source.Name())] {
			continue
		}
		delete(wanted, strings.ToLower(source.Name()))
		active = append(active, source)
	}
	if len(wanted) > 0 {
		unknown := make([]string, 0, len(wanted))
		for name := range wanted {
			unknown = append(unknown, name)
		}
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown source: %s", strings.Join(unknown, ", "))
	}

	results := make([][]Event, len(active))
	var wg sync.WaitGroup
	for i, source := range active {
		wg.Go(func() {
			collected, err := source.Collect(ctx, dateRange)
			if err != nil {
				slog.Warn("skipping source", "source", source.Name(), "error", err)
				return
			}
			results[i] = dateRange.Filter(collected)
		})
	}
	wg.Wait()

	var events []Event
	for _, collected := range results {
		events = append(events, collected...)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Date.Before(events[j].Date) })
	slog.Info("all collectors finished; printing summary", "sources", len(active), "events", len(events))
	return events, nil
}
