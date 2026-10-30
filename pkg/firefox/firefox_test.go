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
			description TEXT
		);
		CREATE TABLE moz_historyvisits (
			id INTEGER PRIMARY KEY,
			place_id INTEGER,
			visit_date INTEGER
		);

		INSERT INTO moz_places VALUES
			(1, 'https://github.com/jovandeginste/personalactivity', '  personalactivity  ', '  Activity CLI  '),
			(2, 'https://example.com/raw', NULL, NULL);

		INSERT INTO moz_historyvisits VALUES
			(10, 1, 1793384819750740),
			(11, 2, 1793385000000000);
	`).Error)

	events, err := New(firefoxDir).Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 2)

	require.True(t, events[0].Date.Equal(time.UnixMicro(1_793_384_819_750_740)))
	require.Equal(t, "firefox", events[0].Source)
	require.Equal(t, "github.com", events[0].Topic)
	require.Equal(t, "personalactivity (https://github.com/jovandeginste/personalactivity)", events[0].Summary)
	require.Equal(t, "https://github.com/jovandeginste/personalactivity", events[0].Attributes["url"])
	require.Equal(t, "personalactivity", events[0].Attributes["title"])
	require.Equal(t, "Activity CLI", events[0].Attributes["description"])

	require.Equal(t, "example.com", events[1].Topic)
	require.Equal(t, "https://example.com/raw", events[1].Summary)
}
