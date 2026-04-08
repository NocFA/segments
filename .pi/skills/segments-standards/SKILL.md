---
name: segments-standards
description: Coding standards for the Segments project. Enforce clean, non-AI-looking code with no needless comments, no em dashes, no emojis. Follow Go idioms.
---

# Segments Coding Standards

Apply these rules to all code written for this project.

## Golden Rules

1. **No AI-written code appearance** - avoid patterns that scream LLM-generated
2. **No rules of 3 giveaways** - don't repeat patterns three times unnecessarily
3. **No needless comments** - code explains itself; comments only for non-obvious decisions
4. **No em dashes** - use standard ASCII punctuation (hyphens, colons)
5. **No emojis or unicode symbols in output** - use `*` not `✓`, plain ASCII
6. **Clean, proper code** - follow Go idioms, proper error handling, meaningful naming

## Go Code

- Short variable names when scope is clear: `err`, `resp`, `txn`
- Return early to reduce nesting
- Handle errors at the call site, not buried in helpers
- Use `strconv.Itoa` not `fmt.Sprintf("%d", n)` for simple int-to-string
- Use helper closures to reduce repetition (e.g. `str := func(key string) string { ... }`)
- Don't duplicate utility functions across packages (one `expandPath`, not two)
- Group related constants; use a data-driven map/table instead of long switch chains where appropriate

## Bubbletea / TUI

- Use `.Run()` not `.Start()` - Start is deprecated and returns nothing
- **Capture the final model**: `final, err := tea.NewProgram(m).Run()` then `final.(myModel).field`
- Value receivers mean the caller's copy is never updated - always use the return from Run
- Make TUI components reusable: a generic `confirm(title, detail)` beats one-off models per prompt
- Reuse style vars from the parent package; don't redeclare duplicate lipgloss styles in separate files

## CLI Design

- Use a command alias map (`var aliases = map[string]string{...}`) instead of nested switch/if chains
- Keep `Run()` as a thin dispatcher: resolve alias, pick handler, done
- Bare `return` in a function returning `error` won't compile - always `return nil`
- User-facing output: `bold`, `cyan`, `dim` styles via lipgloss; no raw ANSI in Go code
- Shell scripts can use raw ANSI (`\033[0;36m`) - keep it simple, no elaborate helper functions

## HTTP / Server

- Use `r.PathValue("id")` (Go 1.22+) instead of hand-parsing URL paths
- Keep handlers focused: parse, call store, write response
- No business logic in handlers

## Shell Scripts

- Simple ANSI: define `C='\033[0;36m'` vars at the top, use `printf "${C}text${R}\n"`
- Don't suppress errors with `2>/dev/null || true` unless there's a real reason
- Don't write elaborate shell functions for things a one-liner handles
- When copying Go binaries on macOS, re-sign with `codesign -fs -` after `cp` - macOS applies `com.apple.provenance` to copies which causes `Killed: 9`

## Testing

- Test behavior, not implementation
- Table-driven tests for variations
- Keep test helper signatures in sync with the functions they call

## What To Avoid

- Comments like `// This is a function` or `// Loop through items`
- Variable names that repeat the type: `userMap`, `taskList`
- `fmt.Sprintf` for trivial formatting that `strconv` handles
- `var _ = io.Discard` or similar import hacks - if you don't need the import, remove it
- Hardcoded absolute paths (`filepath.Join(home, "Dev", "segments", ...)`) - use cwd or config
- Over-abstraction: interfaces with one implementation
- Dead code: if a function isn't called, delete it
- `init()` functions that don't need to exist
