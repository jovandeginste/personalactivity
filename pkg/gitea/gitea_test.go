package gitea

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectSkipsWhenGiteaAPIEnvNotSet(t *testing.T) {
	t.Setenv("GITEA_API", "")

	events, err := New("user").Collect(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestCommitEventsCreatesAnEventForEachCommit(t *testing.T) {
	item := feedEvent{OpType: "commit_repo"}
	item.Repo.FullName = "ceif-lnx/example"
	item.Content = `{"Commits":[{"Sha1":"first","Message":"First commit\n","Timestamp":"2026-10-30T15:29:17+01:00"},{"Sha1":"second","Message":"Second commit","Timestamp":"2026-10-30T16:29:17+01:00"}]}`

	events, err := commitEvents(item)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "First commit", events[0].Summary)
	require.Equal(t, "first", events[0].Attributes["sha1"])
	require.True(t, events[0].Date.Before(events[1].Date))
}
