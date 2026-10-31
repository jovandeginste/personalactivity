package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cli/go-gh/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestCollectSkipsWhenNoTokenFound(t *testing.T) {
	t.Setenv("GH_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_ENTERPRISE_TOKEN", "")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "")
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_PATH", "/nonexistent")
	t.Setenv("PATH", t.TempDir())

	_ = config.Reload(nil)

	events, err := New().Collect(context.Background())
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestCollectFetchesAndConvertsEvents(t *testing.T) {
	t.Setenv("GH_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")

	_ = config.Reload(nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		_, _ = fmt.Fprint(w, `{"login":"jovandeginste"}`)
	})
	mux.HandleFunc("/api/v3/users/jovandeginste/events", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		_, _ = fmt.Fprint(w, `[
			{"id":"1","type":"PushEvent","actor":{"login":"jovandeginste"},"repo":{"name":"jovandeginste/personalactivity"},"created_at":"2026-10-31T10:00:00Z","payload":{"ref":"refs/heads/main","head":"862ed661e0866d84cacab9b2756da3fae5e32038","before":"7f86d067e3a0177f62c94d77c5bf909fa3d0e52d"}},
			{"id":"2","type":"IssueCommentEvent","actor":{"login":"jovandeginste"},"repo":{"name":"jovandeginste/workout-tracker"},"created_at":"2026-10-31T11:00:00Z","payload":{"action":"created","issue":{"number":885,"title":"Fix bug"},"comment":{"body":"Looks good!\n","html_url":"https://github.com/jovandeginste/workout-tracker/issues/885#issuecomment-1"}}}
		]`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	events, err := Source{baseURL: server.URL + "/api/v3/"}.Collect(context.Background())
	require.NoError(t, err)
	require.Len(t, events, 2)

	require.Equal(t, "github", events[0].Source)
	require.Equal(t, "jovandeginste/personalactivity", events[0].Topic)
	require.Equal(t, "pushed 862ed66 to refs/heads/main", events[0].Summary)
	require.Equal(t, "PushEvent", events[0].Attributes["type"])
	require.Equal(t, "refs/heads/main", events[0].Attributes["ref"])

	require.Equal(t, "github", events[1].Source)
	require.Equal(t, "jovandeginste/workout-tracker", events[1].Topic)
	require.Equal(t, "commented on #885 Fix bug: Looks good!", events[1].Summary)
	require.Equal(t, "IssueCommentEvent", events[1].Attributes["type"])
	require.Equal(t, "885", events[1].Attributes["issue"])
}
