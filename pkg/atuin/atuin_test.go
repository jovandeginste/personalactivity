package atuin

import (
	"os/user"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
