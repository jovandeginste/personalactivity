// Package activity provides the shared model and collection logic for activity sources.
package activity

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	Collect(context.Context) ([]Event, error)
}

type Range struct {
	From *time.Time
	To   *time.Time // exclusive
}

func ParseRange(from, to string) (Range, error) {
	if from == "" && to == "" {
		now := time.Now()
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return Range{From: &midnight, To: &now}, nil
	}
	start, err := parseDate(from, false)
	if err != nil {
		return Range{}, fmt.Errorf("invalid --from: %w", err)
	}
	end, err := parseDate(to, true)
	if err != nil {
		return Range{}, fmt.Errorf("invalid --to: %w", err)
	}
	if start != nil && end != nil && !start.Before(*end) {
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

	var events []Event
	for _, source := range sources {
		if len(wanted) > 0 && !wanted[strings.ToLower(source.Name())] {
			continue
		}
		delete(wanted, strings.ToLower(source.Name()))
		collected, err := source.Collect(ctx)
		if err != nil {
			return nil, fmt.Errorf("collect %s activity: %w", source.Name(), err)
		}
		for _, event := range collected {
			if (dateRange.From == nil || !event.Date.Before(*dateRange.From)) && (dateRange.To == nil || event.Date.Before(*dateRange.To)) {
				events = append(events, event)
			}
		}
	}
	if len(wanted) > 0 {
		unknown := make([]string, 0, len(wanted))
		for name := range wanted {
			unknown = append(unknown, name)
		}
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown source: %s", strings.Join(unknown, ", "))
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Date.Before(events[j].Date) })
	return events, nil
}
