package teams

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/jovandeginste/personalactivity/pkg/activity"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCollectSentAndReceivedMessages(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "teams.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{}) //nolint:exhaustruct_v5
	require.NoError(t, err)

	require.NoError(t, db.Exec(`
		CREATE TABLE authors (id TEXT PRIMARY KEY, name TEXT);
		CREATE TABLE channels (id TEXT PRIMARY KEY, name TEXT, type TEXT);
		CREATE TABLE messages (
			id INTEGER PRIMARY KEY,
			thread_id INTEGER,
			channel_id TEXT,
			direction TEXT,
			author_id TEXT,
			content TEXT,
			reactions TEXT,
			timestamp TIMESTAMP,
			recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO authors VALUES ('8:orgid:1', 'Alice'), ('8:orgid:2', 'Jo Vandeginste');
		INSERT INTO channels VALUES ('19:chan1@thread.tacv2', 'General', 'channel');
		INSERT INTO messages (id, thread_id, channel_id, direction, author_id, content, reactions) VALUES
			(1700000000000, 1700000000000, '19:chan1@thread.tacv2', 'received', '8:orgid:1', '  hello   world  ', '👍 1'),
			(1700000030000, NULL, '19:chan1@thread.tacv2', 'event', NULL, 'Call started', NULL),
			(1700000060000, 1700000000000, '19:chan1@thread.tacv2', 'sent', '8:orgid:2', 'reply here', NULL);
	`).Error)

	events, err := New(dbPath).Collect(context.Background(), activity.Range{}) //nolint:exhaustruct_v5
	require.NoError(t, err)
	require.Len(t, events, 2)

	require.Equal(t, time.UnixMilli(1700000000000).Local(), events[0].Date)
	require.Equal(t, "teams", events[0].Source)
	require.Equal(t, "General", events[0].Topic)
	require.Equal(t, "Alice: hello world", events[0].Summary)
	require.Equal(t, "received", events[0].Attributes["direction"])
	require.Equal(t, "Alice", events[0].Attributes["author"])
	require.Equal(t, "hello   world", events[0].Attributes["content"])
	require.Equal(t, "👍 1", events[0].Attributes["reactions"])
	require.Equal(t, "1700000000000", events[0].Attributes["thread_id"])

	require.Equal(t, time.UnixMilli(1700000060000).Local(), events[1].Date)
	require.Equal(t, "Jo Vandeginste: reply here", events[1].Summary)
	require.Equal(t, "sent", events[1].Attributes["direction"])
}
