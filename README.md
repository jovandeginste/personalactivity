# Personal Activity

`personalactivity` is a CLI tool that collects your personal activity from configured local and remote sources into a unified chronological feed.

## Overview

When summarizing a workday or checking what you worked on during a specific period, relevant context is spread across git forges, shell history, editor undo history, AI coding sessions, emails, calendar events, and browser history. `personalactivity` reads these sources (in read-only mode where applicable), filters events by date range and source, and outputs them as a table or JSON.

### Supported Sources

- **`gitea`**: Activity feed and commits for the current OS user from the Gitea API server configured in `GITEA_API`, authenticated via `httpsig` using the SSH certificate in `ssh-agent`.
- **`github`**: User activity feed from the GitHub REST API via `github.com/google/go-github`, authenticated using `gh` CLI authentication logic (`github.com/cli/go-gh`).
- **`atuin`**: Local shell command history read in read-only mode from `~/.local/share/atuin/history.db`.
- **`vimundo`**: Neovim/Vim undo history files parsed from `~/.local/state/nvf/undo`.
- **`opencode`**: User prompts and session metadata read in read-only mode from `~/.local/share/opencode/opencode.db`.
- **`thunderbird`**: Sent emails (timestamps, sender and destination addresses, subjects, and bodies) from `~/.thunderbird/*/global-messages-db.sqlite` and calendar events (timestamps, calendar names, titles, locations, descriptions, organizers, and attendees) from `~/.thunderbird/*/calendar-data/cache.sqlite`, read in read-only mode.
- **`firefox`**: Visited URLs (visit timestamps, hosts, page titles, URLs, and descriptions) read in read-only mode from `places.sqlite` in the default Firefox profile resolved via `~/.mozilla/firefox/profiles.ini`.

## Implementation

The project is written in Go and structured into reusable packages under `./pkg/` with the CLI entrypoint in `./cmd/personalactivity/`:

- `pkg/activity`: Shared `Event` model, `Source` interface, date range parsing, filtering, sorting, and `table`/`json` output formatting.
- `pkg/gitea`: Queries the Gitea user activity feed authenticated with `github.com/42wim/httpsig` and `golang.org/x/crypto/ssh/agent`.
- `pkg/github`: Queries the GitHub user activity feed using `github.com/google/go-github` and `github.com/cli/go-gh/v2/pkg/auth`.
- `pkg/atuin`: Queries the local SQLite database via `gorm` and `glebarez/sqlite` (pure Go SQLite).
- `pkg/vimundo`: Binary parser for Vim's `VimˆUnDo` undo file format.
- `pkg/opencode`: Queries user session messages from the local OpenCode SQLite database.
- `pkg/thunderbird`: Queries sent emails (`global-messages-db.sqlite`) and calendar events (`calendar-data/cache.sqlite`) from local Thunderbird profiles.
- `pkg/firefox`: Resolves the default Firefox profile from `profiles.ini` and queries visited URLs and metadata from `places.sqlite` in read-only mode.

## Build and Install

Use the provided `Makefile` targets:

```bash
# Build ./personalactivity binary
make build

# Install into $GOBIN / $GOPATH/bin
make install

# Run unit tests
make test

# Run linter
make lint
```

Prebuilt Linux `amd64` binaries are also published on [GitHub Releases](https://github.com/jovandeginste/personalactivity/releases).

## Usage

By default (when `--from` and `--to` are omitted), `personalactivity` collects activity from all sources starting from midnight today (`00:00:00` local time) up to the current time.

```bash
personalactivity [flags]
```

### Flags

- `--from string`: Start date (`YYYY-MM-DD` or `RFC3339`).
- `--to string`: End date (`YYYY-MM-DD` or `RFC3339`). When given as `YYYY-MM-DD`, the entire end day is included.
- `-s, --source strings`: Source(s) to collect (`gitea`, `github`, `atuin`, `vimundo`, `opencode`, `thunderbird`, `firefox`). Can be repeated or comma-separated. Defaults to all sources.
- `-o, --format string`: Output format: `table` or `json` (default `table`).

### Examples

Collect today's activity across all sources as a table:

```bash
personalactivity
```

Collect activity for a specific date in JSON format:

```bash
personalactivity --from 2026-10-30 --to 2026-10-30 --format json
```

Collect only Gitea and OpenCode activity since a given date:

```bash
personalactivity --from 2026-10-25 -s gitea -s opencode
```
