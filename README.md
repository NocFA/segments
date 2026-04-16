# Segments

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A performant, lightweight, and scalable alternative to Beads, written in Go.

## Why

Got fed up with Beads. It's slow, it's heavy, and it's written in the wrong language. Segments does the same job, faster, in a single binary, without making you question your life choices.

## Features

- Projects and tasks with priorities, statuses, and rich text bodies
- Kanban board and list view in the web UI
- Real-time updates over WebSocket
- MCP server for Claude Code integration (auto-starts server, injects session context)
- Pi extension for task-aware AI sessions
- OpenCode MCP integration
- Auto-start service (opt-in: launchd / systemd / Windows Task Scheduler)
- Single binary, no external dependencies at runtime

## Install

### macOS / Linux

```bash
curl -fsSL https://git.nocfa.net/NocFA/segments/raw/branch/main/scripts/install.sh | bash
```

Downloads a pre-built binary for your platform. Falls back to building from source if no binary is available.

**Prerequisites (source build only):** [Go 1.24+](https://go.dev/dl/), a C compiler (for LMDB)

### Windows

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

Or clone and run:

```powershell
git clone https://git.nocfa.net/NocFA/segments.git
cd segments
powershell -ExecutionPolicy Bypass -File .\scripts\install.ps1
```

The install script automatically detects and installs missing dependencies via [winget](https://learn.microsoft.com/en-us/windows/package-manager/winget/):

- **[Go 1.24+](https://go.dev/dl/)** -- `winget install GoLang.Go`
- **[MinGW-w64 (GCC)](https://winlibs.com/)** -- `winget install BrechtSanders.WinLibs.POSIX.UCRT`

**Already present on modern Windows:** Git, winget (ships with Windows 10/11).

The script builds from local source when run from the repo, otherwise clones and builds. Installs to `%USERPROFILE%\.local\bin` and adds it to the user PATH.

### From source (any platform)

```bash
git clone https://git.nocfa.net/NocFA/segments.git
cd segments
make install
```

Requires Go 1.24+ and a C compiler (CGO_ENABLED=1 for LMDB).

## Quick Start

```bash
sg setup    # configure integrations (Pi, Claude Code, OpenCode)
sg start    # start the server
```

Open `http://localhost:8765`.

## Usage

```
sg help

  Server
    start         start the server
    stop          stop the server

  Tasks
    list          list projects and tasks
    view          view full task details
    add           create a task
    done          mark a task as done
    close         close a task
    rename        rename a project

  Setup
    setup         configure integrations
    init          add integrations to current project
    beads         import tasks from Beads
    uninstall     remove segments and all data

  Info
    help          show help
    version       print version
```

`sg list` auto-detects the current project from the working directory name. Pass `-a` to include completed tasks. Passing a task ID prefix to `list` falls through to `view`.

`sg view <id>` shows full task details: title, status, priority, blockers, dates, and body.

### Integrations

| Tool | How | Status |
|------|-----|--------|
| Claude Code | MCP server + SessionStart hook | Working |
| Pi | Embedded TypeScript extension | Working |
| OpenCode | MCP server | Working |

Run `sg setup` to configure globally, or `sg init` in a project directory for local config.

The Claude Code integration provides both MCP tools (create/update/list/delete tasks) and a SessionStart hook that injects current project context into every new session.

## Configuration

Config lives at `~/.segments/config.yaml` (or `$SEGMENTS_DATA_DIR`):

```yaml
port: "8765"
bind: "127.0.0.1"
data_dir: "~/.segments"
```

## Development

```bash
make build    # build binary
make install  # build and install to ~/.local/bin
```

Cross-compile:

```bash
make cross-linux-amd64   # static linux binary (requires musl-gcc)
make cross-linux-arm64   # linux arm64
```

## Platforms

| OS | Arch | Status |
|----|------|--------|
| macOS | arm64 | CI tested |
| Linux | amd64 | CI tested |
| Linux | arm64 | Cross-compiles |
| Windows | amd64 | Working (native build with MinGW-w64) |

## License

[MIT](LICENSE)
