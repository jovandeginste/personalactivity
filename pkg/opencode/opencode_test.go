package opencode

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEventUsesUserMessageText(t *testing.T) {
	source := New("unused")
	event, ok, err := source.event(message{
		SessionID: "ses_1", TimeCreated: 1_793_378_046_797,
		Data:      `{"text":"  Add an activity source  "}`,
		Directory: "/tmp/project", Title: "Activity source", Agent: "build", Model: "gpt-5.6-terra",
	})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "Add an activity source", event.Summary)
	require.Equal(t, "/tmp/project", event.Topic)
	require.True(t, event.Date.Equal(time.UnixMilli(1_793_378_046_797)))
	require.Equal(t, "ses_1", event.Attributes["session"])
}
