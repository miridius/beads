# Beads (bd) - Project Instructions for AI Agents

**bd** is a distributed graph issue tracker for AI agents, powered by Dolt.
Written in Go. CLI entry point: `cmd/bd/`.

## Build & Test

```bash
# Build
make build                      # Builds ./bd binary (CGO required)

# Test (preferred — uses skip list and coverage)
make test                       # Runs ./scripts/test.sh with coverage
make test-full-cgo              # Full CGO suite via ./scripts/test-cgo.sh

# Test specific packages
go test ./internal/storage/dolt/ -v
./scripts/test-cgo.sh ./cmd/bd/...

# Lint
golangci-lint run ./...         # Config: .golangci.yml

# Format
make fmt                        # gofmt -w .
make fmt-check                  # CI check (exits non-zero if unformatted)
```

CGO is required (Dolt backend). On macOS, ICU flags are auto-configured by
the Makefile. Do not run raw `CGO_ENABLED=1 go test` on macOS without ICU
flags — use `make test` or `./scripts/test-cgo.sh` instead.

## Project Structure

```
cmd/bd/              # CLI commands (Cobra). Add new commands here.
internal/
  types/             # Core data types (Issue, Dependency, etc.)
  storage/           # Storage interface
    dolt/            # Dolt database backend (CGO)
integrations/
  beads-mcp/         # MCP server for Claude and other AI assistants (Python)
examples/            # Integration examples
scripts/             # Build, test, release, and utility scripts
```

## Code Conventions

- Follow existing patterns in `cmd/bd/` for new CLI commands
- Add `--json` flag to all commands for programmatic use
- Use table-driven tests; use `t.TempDir()` to avoid polluting production DB
- Never create test issues in the production Dolt database
- Run `golangci-lint run ./...` before committing
- Update docs when changing CLI behavior

## Version Management

- Source of truth: `version.go`
- Bump all version files atomically: `./scripts/update-versions.sh X.Y.Z`
- Verify consistency: `./scripts/check-versions.sh`

## Fork & PR Workflow

If contributing from a fork:
- Add the upstream remote: `git remote add upstream https://github.com/steveyegge/beads.git`
- Base branches on `upstream/main`, not your fork's `main`
- Keep PRs focused: one issue per PR, clean commit history
- Run `make test` and `make fmt-check` before opening a PR

## Key Documentation

- **AGENTS.md** / **AGENT_INSTRUCTIONS.md** — detailed AI agent workflows
- **CONTRIBUTING.md** — development setup and guidelines
- **README.md** — user-facing documentation
- **.github/copilot-instructions.md** — GitHub Copilot-specific guidance
