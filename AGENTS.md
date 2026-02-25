# Repository Guidelines

## Project Structure & Module Organization
- `cmd/picoclaw/`: CLI entrypoint and command wiring.
- `pkg/`: Core application modules (agent loop, providers, tools, channels, config, routing, session, skills, state).
- `config/`: Config templates such as `config.example.json`.
- `docs/`: Channel setup guides and design/migration notes.
- `assets/`: Images and media used in documentation.
- Tests are colocated with code as `*_test.go` files under `pkg/**` (for example, `pkg/tools/filesystem_test.go`).

## Build, Test, and Development Commands
- `make deps`: Download and verify Go dependencies.
- `make generate`: Run `go generate ./...` and refresh generated artifacts.
- `make build`: Build current platform binary to `build/picoclaw-<os>-<arch>` and symlink `build/picoclaw`.
- `make build-all`: Cross-build binaries for major target platforms.
- `make test`: Run all unit tests (`go test ./...`).
- `make fmt`: Apply project formatters (`gofumpt`, `goimports`, `gci`, `golines`).
- `make lint`: Run `golangci-lint`.
- `make check`: Pre-PR gate (`deps + fmt + vet + test`).
- Integration tests: `go test -tags=integration ./pkg/providers/...`.

## Coding Style & Naming Conventions
- Language baseline is Go `1.25.x` (`go.mod`).
- Follow idiomatic Go naming: exported `CamelCase`, internal `camelCase`, package names lowercase.
- Keep files and modules focused; avoid unnecessary abstractions (YAGNI/KISS).
- Run `make fmt` before committing; lint config enforces import ordering and a 120-char line limit.

## Testing Guidelines
- Use table-driven tests where practical and keep tests near the code they validate.
- Test names should follow `TestXxx` (and `BenchmarkXxx` for benchmarks).
- For targeted runs, use commands like `go test -run TestSpawn ./pkg/tools`.
- Integration tests are build-tagged (`//go:build integration`) and may require external CLIs (for example `codex` or `claude`) in `PATH`.

## Commit & Pull Request Guidelines
- Use Conventional Commit prefixes seen in history: `fix:`, `feat:`, `refactor:`, `docs:`, `perf:`.
- Write concise imperative commit subjects in English and reference issues when relevant (for example, `fix: handle timeout (#123)`).
- Branch from `main` and open PRs back to `main`; do not push directly to `main` or `release/*`.
- Before opening a PR, run `make check` and complete `.github/pull_request_template.md` (description, change type, AI disclosure, issue link, test environment, checklist).

## Security & Configuration Tips
- Start from `config/config.example.json` or `.env.example`; keep real credentials local only.
- Never commit API keys, tokens, or private endpoints.
- Treat tool/channel input handling as security-sensitive, especially filesystem paths and shell execution paths.
