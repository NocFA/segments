# Segments

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A performant, lightweight, and scalable alternative to Beads, written in Go.

## Why

Got fed up with Beads. It's slow, it's heavy, and it's written in the wrong language. Segments does the same job, faster, in a single binary, without making you question your life choices.

## Features

- Projects and tasks with priorities, statuses, and rich text bodies
- Kanban board and list view in the web UI
- Real-time updates over WebSocket
- MCP server for Claude Code integration
- Pi extension for task-aware AI sessions
- Single binary, no external dependencies at runtime

## Quick Start

```bash
curl -fsSL https://git.nocfa.net/NocFA/segments/raw/branch/main/scripts/install.sh | bash
```

Installs a pre-built binary for your platform. Falls back to building from source if no binary is available (requires Go 1.22+).

Then:

```bash
segments serve    # start the server
segments setup    # configure integrations
```

Open `http://localhost:8765`.

## Install from source

```bash
git clone https://git.nocfa.net/NocFA/segments.git
cd segments
make install
```

Requires Go 1.22+ and CGO (for LMDB).

## Usage

```bash
segments serve          # start server on :8765
segments init           # create default config
segments setup          # configure integrations (Pi, Claude Code)
```

### CLI

```bash
segments list                          # list projects
segments add <project-id> <title>      # add a task
segments done <project-id> <task-id>   # mark done
segments stop                          # stop server
```

### Integrations

| Tool | Status |
|------|--------|
| Pi | Working |
| Claude Code (MCP) | Working |
| OpenCode (MCP) | WIP |

Run `segments setup` in your project directory to configure available integrations automatically.

## Configuration

Config lives at `~/.segments/config.yaml` (or `$SEGMENTS_DATA_DIR`):

```yaml
addr: :8765
```

## Development

```bash
make build    # build binary
make install  # build and install to ~/.local/bin
```

## License

[MIT](LICENSE)
