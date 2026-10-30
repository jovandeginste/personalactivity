package atuin

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTimestampSupportsSecondsAndNanoseconds(t *testing.T) {
	want := time.Unix(1_700_000_000, 0)
	require.True(t, timestamp(1_700_000_000).Equal(want))
	require.True(t, timestamp(want.UnixNano()).Equal(want))
}

func TestHomePath(t *testing.T) {
	currentUser, err := user.Current()
	require.NoError(t, err)
	require.Equal(t, "~/project", homePath(currentUser.HomeDir+"/project"))
	require.Equal(t, "~/project", homePath("/different/root/"+currentUser.Username+"/project"))
	path := "/a/b/" + currentUser.Username + "/c/d/" + currentUser.Username + "/e/f"
	require.Equal(t, "~/c/d/"+currentUser.Username+"/e/f", homePath(path))
	require.Equal(t, "/tmp/project", homePath("/tmp/project"))
}

func TestCollectFiltersByCurrentHostname(t *testing.T) {
	hostname, err := os.Hostname()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&history{}))
	require.NoError(t, db.Create([]history{
		{Timestamp: 1_700_000_000, Command: "included", Hostname: hostname + ":session"},
		{Timestamp: 1_700_000_001, Command: "excluded", Hostname: "other:session"},
		{Timestamp: 1_700_000_002, Command: "excluded", Hostname: hostname},
	}).Error)

	events, err := New(path).Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "included", events[0].Summary)
}
