package vimundo

import (
	"os/user"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseLocalUndoFiles(t *testing.T) {
	currentUser, err := user.Current()
	require.NoError(t, err)
	home := currentUser.HomeDir
	files, err := filepath.Glob(filepath.Join(home, ".local/state/nvf/undo/*"))
	require.NoError(t, err)
	if len(files) == 0 {
		t.Skip("no local Neovim undo files")
	}

	for _, name := range files {
		undo, err := ReadFile(name)
		require.NoError(t, err, "parse %s", name)
		require.Contains(t, []uint16{2, 3}, undo.Version, name)
		require.NotEmpty(t, undo.Headers, name)
	}
}

func TestOriginalPath(t *testing.T) {
	currentUser, err := user.Current()
	require.NoError(t, err)
	require.Equal(t, "/different/root/"+currentUser.Username+"/notes.md", originalPath("%different%root%"+currentUser.Username+"%notes.md"))
}
