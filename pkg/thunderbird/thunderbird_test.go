package thunderbird

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCollectSentMessages(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "global-messages-db.sqlite")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.Exec(`
		CREATE TABLE folderLocations (id INTEGER PRIMARY KEY, folderURI TEXT NOT NULL, dirtyStatus INTEGER NOT NULL, name TEXT NOT NULL, indexingPriority INTEGER NOT NULL);
		CREATE TABLE messages (id INTEGER PRIMARY KEY, folderID INTEGER, messageKey INTEGER, conversationID INTEGER NOT NULL, date INTEGER, headerMessageID TEXT, deleted INTEGER NOT NULL default 0, jsonAttributes TEXT, notability INTEGER NOT NULL default 0);
		CREATE TABLE messagesText_content (docid INTEGER PRIMARY KEY, c0body TEXT, c1subject TEXT, c2attachmentNames TEXT, c3author TEXT, c4recipients TEXT);
		CREATE TABLE identities (id INTEGER PRIMARY KEY, contactID INTEGER NOT NULL, kind TEXT NOT NULL, value TEXT NOT NULL, description TEXT NOT NULL, relay INTEGER NOT NULL);

		INSERT INTO folderLocations VALUES
			(1, 'imap://user@example.com/INBOX', 0, 'INBOX', 50),
			(2, 'imap://user@example.com/Sent', 0, 'Sent', 60),
			(3, 'imap://user@example.com/Verzonden items', 0, 'Verzonden items', 60);

		INSERT INTO identities VALUES
			(1, 1, 'email', 'jo@example.com', 'Jo', 0),
			(2, 2, 'email', 'alice@example.com', 'Alice', 0),
			(3, 3, 'email', 'bob@example.com', 'Bob', 0);

		INSERT INTO messages VALUES
			(10, 1, 1, 100, 1793280000000000, 'inbox-1@example.com', 0, '{"43":2,"44":[1],"53":[1]}', 0),
			(11, 2, 2, 101, 1793280049000000, 'sent-1@example.com', 0, '{"43":1,"44":[2],"45":[3],"53":[2,3]}', 0),
			(12, 3, 3, 101, 1793280049000000, 'sent-1@example.com', 0, '{"43":1,"44":[2],"45":[3],"53":[2,3]}', 0),
			(13, 2, 4, 102, 1793280099000000, 'deleted-1@example.com', 1, '{"43":1,"44":[2],"53":[2]}', 0);

		INSERT INTO messagesText_content VALUES
			(10, 'Hello Jo', 'Incoming mail', '', 'Alice', 'Jo'),
			(11, '  Here is the latest status.  ', '  Project update  ', '', 'Jo', 'Alice Bob'),
			(12, '  Here is the latest status.  ', '  Project update  ', '', 'Jo', 'Alice Bob'),
			(13, 'Old draft', 'Deleted mail', '', 'Jo', 'Alice');
	`).Error)

	events, err := New(dbPath).Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 1)

	event := events[0]
	require.True(t, event.Date.Equal(time.UnixMicro(1_793_280_049_000_000)))
	require.Equal(t, "thunderbird", event.Source)
	require.Equal(t, "alice@example.com, bob@example.com", event.Topic)
	require.Equal(t, "Project update: Here is the latest status.", event.Summary)
	require.Equal(t, "jo@example.com", event.Attributes["from"])
	require.Equal(t, "alice@example.com", event.Attributes["to"])
	require.Equal(t, "bob@example.com", event.Attributes["cc"])
	require.Equal(t, "Project update", event.Attributes["subject"])
	require.Equal(t, "Here is the latest status.", event.Attributes["body"])
	require.Equal(t, "sent-1@example.com", event.Attributes["message_id"])
}

func TestCleanBodyRemovesAttachmentsAndTruncates(t *testing.T) {
	raw := "Hello ![plan](data:image/png;base64,AAAA) world [data:image/png;base64,BBBB] " + strings.Repeat("x", 1200)
	cleaned := cleanBody(raw)
	require.NotContains(t, cleaned, "data:image")
	require.True(t, strings.HasPrefix(cleaned, "Hello  world "))
	require.Len(t, []rune(cleaned), 1000)
}

