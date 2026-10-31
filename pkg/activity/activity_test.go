package activity

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type testSource struct {
	name   string
	events []Event
	err    error
}

func (s testSource) Name() string                                    { return s.name }
func (s testSource) Collect(_ context.Context, r Range) ([]Event, error) { return r.Filter(s.events), s.err }

func TestCollectFiltersAndSorts(t *testing.T) {
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	events, err := Collect(context.Background(), []Source{
		testSource{name: "first", events: []Event{{Date: base.Add(2 * time.Hour)}}},
		testSource{name: "second", events: []Event{{Date: base.Add(time.Hour)}}},
	}, []string{"first", "second"}, Range{From: &base})
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.True(t, events[0].Date.Equal(base.Add(time.Hour)))
}

func TestCollectFiltersToSelectedSource(t *testing.T) {
	events, err := Collect(context.Background(), []Source{
		testSource{name: "selected", events: []Event{{}}},
		testSource{name: "unselected", events: []Event{{}}},
	}, []string{"selected"}, Range{})
	require.NoError(t, err)
	require.Len(t, events, 1)
}

func TestParseRangeIncludesWholeToDate(t *testing.T) {
	rangeFilter, err := ParseRange("2026-10-30", "2026-10-30")
	require.NoError(t, err)
	require.Equal(t, "2026-10-31T00:00:00Z", rangeFilter.To.Format(time.RFC3339))
}

func TestCollectRejectsUnknownSource(t *testing.T) {
	_, err := Collect(context.Background(), []Source{testSource{name: "known"}}, []string{"unknown"}, Range{})
	require.Error(t, err)
}

func TestCollectSkipsFailingSource(t *testing.T) {
	events, err := Collect(context.Background(), []Source{
		testSource{name: "failing", err: context.DeadlineExceeded},
		testSource{name: "working", events: []Event{{Summary: "ok"}}},
	}, nil, Range{})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "ok", events[0].Summary)
}

func TestParseRangeDefaultsToToday(t *testing.T) {
	rangeFilter, err := ParseRange("", "")
	require.NoError(t, err)
	require.Equal(t, 0, rangeFilter.From.Hour())
	require.Equal(t, 0, rangeFilter.From.Minute())
	require.Equal(t, 0, rangeFilter.From.Second())
	require.False(t, rangeFilter.To.Before(*rangeFilter.From))
	require.False(t, rangeFilter.To.After(time.Now().Add(time.Second)))
}

func TestParseRangeDefaultsToWhenOnlyFromIsSet(t *testing.T) {
	rangeFilter, err := ParseRange("2026-10-25", "")
	require.NoError(t, err)
	require.Equal(t, "2026-10-25T00:00:00Z", rangeFilter.From.Format(time.RFC3339))
	require.NotNil(t, rangeFilter.To)
	require.False(t, rangeFilter.To.After(time.Now().Add(time.Second)))
}

func TestWriteJSONOmitsEmptyAttributes(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, []Event{{
		Date:    time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC),
		Source:  "firefox",
		Topic:   "example.com",
		Summary: "https://example.com",
		Attributes: map[string]string{
			"url":         "https://example.com",
			"title":       "",
			"description": "   ",
		},
	}}, "json")
	require.NoError(t, err)
	require.Contains(t, buf.String(), `"url":"https://example.com"`)
	require.NotContains(t, buf.String(), `"title"`)
	require.NotContains(t, buf.String(), `"description"`)
}
