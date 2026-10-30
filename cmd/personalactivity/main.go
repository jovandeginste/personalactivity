package main

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/jovandeginste/personalactivity/pkg/activity"
	"github.com/jovandeginste/personalactivity/pkg/atuin"
	"github.com/jovandeginste/personalactivity/pkg/gitea"
	"github.com/jovandeginste/personalactivity/pkg/opencode"
	"github.com/jovandeginste/personalactivity/pkg/vimundo"
	"github.com/spf13/cobra"
)

func main() {
	var from, to, format string
	var sourceNames []string

	cmd := &cobra.Command{
		Use:   "personalactivity",
		Short: "Collect personal activity from configured sources",
		RunE: func(_ *cobra.Command, _ []string) error {
			currentUser, err := user.Current()
			if err != nil {
				return fmt.Errorf("determine current user: %w", err)
			}
			rangeFilter, err := activity.ParseRange(from, to)
			if err != nil {
				return err
			}

			sources := []activity.Source{
				gitea.New(currentUser.Username),
				atuin.New(defaultAtuinPath(currentUser.HomeDir)),
				vimundo.New(defaultVimUndoPath(currentUser.HomeDir)),
				opencode.New(defaultOpenCodePath(currentUser.HomeDir)),
			}
			events, err := activity.Collect(context.Background(), sources, sourceNames, rangeFilter)
			if err != nil {
				return err
			}
			return activity.Write(os.Stdout, events, format)
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "start date (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringVar(&to, "to", "", "end date (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringSliceVarP(&sourceNames, "source", "s", nil, "source to collect (gitea, atuin, vimundo, or opencode; repeatable)")
	cmd.Flags().StringVarP(&format, "format", "o", "table", "output format: table or json")
	cmd.InitDefaultCompletionCmd()

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultAtuinPath(home string) string {
	return filepath.Join(home, ".local", "share", "atuin", "history.db")
}

func defaultVimUndoPath(home string) string {
	return filepath.Join(home, ".local", "state", "nvf", "undo")
}

func defaultOpenCodePath(home string) string {
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}
