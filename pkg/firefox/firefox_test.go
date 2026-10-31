package firefox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCollectFromDefaultProfile(t *testing.T) {
	firefoxDir := t.TempDir()
	otherDir := filepath.Join(firefoxDir, "other.profile")
	defaultDir := filepath.Join(firefoxDir, "default.profile")
	require.NoError(t, os.MkdirAll(otherDir, 0o755))
	require.NoError(t, os.MkdirAll(defaultDir, 0o755))

	profilesINI := `[General]
StartWithLastProfile=1
Version=2

[Profile0]
Name=other
IsRelative=1
Path=other.profile

[Profile1]
Name=default
IsRelative=1
Path=default.profile
Default=1
`
	require.NoError(t, os.WriteFile(filepath.Join(firefoxDir, "profiles.ini"), []byte(profilesINI), 0o600))

	dbPath := filepath.Join(defaultDir, "places.sqlite")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.Exec(`
		CREATE TABLE moz_places (
			id INTEGER PRIMARY KEY,
			url LONGVARCHAR,
			title LONGVARCHAR,
			description TEXT,
			hidden INTEGER DEFAULT 0
		);
		CREATE TABLE moz_historyvisits (
			id INTEGER PRIMARY KEY,
			from_visit INTEGER DEFAULT 0,
			place_id INTEGER,
			visit_date INTEGER,
			visit_type INTEGER DEFAULT 1
		);

		INSERT INTO moz_places VALUES
			(1, 'https://github.com/jovandeginste/personalactivity?client=firefox-b-d&hs=abc&utm_source=test', '  personalactivity  ', '  Activity CLI  ', 0),
			(2, 'https://example.com/raw?q=keep&sxsrf=drop', NULL, NULL, 0),
			(3, 'https://github.com/jovandeginste/personalactivity#readme', '  personalactivity  ', '', 0),
			(4, 'https://example.com/hidden-redirect', 'Hidden', '', 1),
			(5, 'https://example.com/redirect-source', 'Redirect Source', '', 0);

		INSERT INTO moz_historyvisits VALUES
			(10, 0, 1, 1793384819750740, 1),
			(11, 0, 5, 1793384824000000, 1),
			(12, 11, 2, 1793384825000000, 6),
			(13, 0, 4, 1793384826000000, 1),
			(14, 10, 3, 1793384829750740, 1);
	`).Error)

	events, err := New(firefoxDir).Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 2)

	require.Equal(t, "example.com", events[0].Topic)
	require.Equal(t, "https://example.com/raw?q=keep", events[0].Summary)
	require.Equal(t, "redirect_temporary", events[0].Attributes["visit_type"])
	require.Empty(t, events[0].Attributes["count"])
	require.Empty(t, events[0].Attributes["first_visit"])
	require.Empty(t, events[0].Attributes["last_visit"])

	firstVisit := time.UnixMicro(1_793_384_819_750_740)
	lastVisit := time.UnixMicro(1_793_384_829_750_740)
	require.True(t, events[1].Date.Equal(lastVisit))
	require.Equal(t, "firefox", events[1].Source)
	require.Equal(t, "github.com", events[1].Topic)
	require.Equal(t, "personalactivity (https://github.com/jovandeginste/personalactivity#readme)", events[1].Summary)
	require.Equal(t, "https://github.com/jovandeginste/personalactivity#readme", events[1].Attributes["url"])
	require.Equal(t, "personalactivity", events[1].Attributes["title"])
	require.Equal(t, "Activity CLI", events[1].Attributes["description"])
	require.Equal(t, "link", events[1].Attributes["visit_type"])
	require.Equal(t, "2", events[1].Attributes["count"])
	require.Equal(t, firstVisit.Format(time.RFC3339), events[1].Attributes["first_visit"])
	require.Equal(t, lastVisit.Format(time.RFC3339), events[1].Attributes["last_visit"])
}
