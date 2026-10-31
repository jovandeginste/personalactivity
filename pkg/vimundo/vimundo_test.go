package vimundo

import (
	"os/user"
	"path/filepath"
	"testing"
	"time"

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

func TestCompactHeaders(t *testing.T) {
	base := time.Date(2026, 10, 31, 8, 45, 0, 0, time.UTC)
	compacted := compactHeaders([]Header{
		{Sequence: 1, Time: base, Entries: []Entry{{Top: 10, Bottom: 12}}},
		{Sequence: 2, Time: base.Add(2 * time.Second), Entries: []Entry{{Top: 10, Bottom: 12, Lines: []string{"  draft  "}}}},
		{Sequence: 3, Time: base.Add(5 * time.Second), Entries: []Entry{{Top: 10, Bottom: 12, Lines: []string{"  final line  "}}}},
		{Sequence: 4, Time: base.Add(10 * time.Second), Entries: []Entry{{Top: 20, Bottom: 21, Lines: []string{"first {"}}}},
		{Sequence: 5, Time: base.Add(10 * time.Second), Entries: []Entry{{Top: 20, Bottom: 21, Lines: []string{"};"}}}},
	})
	require.Len(t, compacted, 2)
	require.Equal(t, "final line", formatSummary(compacted[0].lines))
	require.Equal(t, uint32(3), compacted[0].header.Sequence)
	require.Equal(t, 3, compacted[0].entries)
	require.Equal(t, "first { };", formatSummary(compacted[1].lines))
}
