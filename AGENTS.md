# AGENTS.md — Berth

## What This Is

Berth generates Dockerfiles (and related deployment files) from templates for Laravel applications and sets up Coolify resources.

One-line pitch: **Berth is a Dockerfile + Coolify resource generator for Laravel.**

Status: CLI + library are the current focus. A GUI desktop app will come later — keep the library GUI-agnostic.

## Mental Model / How It Works

```
User points at project path
        ↓
Detect  — Scan filesystem (composer.json, package.json, lockfiles, config/inertia.php, etc.)
        ↓
Plan    — Figure out what the project needs (Plan/Project struct)
        ↓
Check   — Validate plan, surface errors/warnings/infos with auto-fix where possible
        ↓
Choices — Prompt user for unresolved decisions (e.g., --workers count, existing vs new DB)
        ↓
Generate — Render templates (go:embed + text/template) → Dockerfile, .dockerignore, entrypoint, nginx/supervisord if needed, compose
        ↓
Coolify  — Hybrid: generate Coolify-compatible files AND optionally call Coolify API to create/update resources (app, workers, db/redis)
```

## Repository Layout

```
cmd/berth-cli/   — Thin CLI wrapper. Flag parsing, I/O, exit codes. No business logic.
pkg/detect/      — Filesystem scanning. Reads composer.json, package.json, lockfiles, configs. Returns Plan.
pkg/plan/        — Core domain types (Plan, Project, Report, Result). Plan.Check() validates.
pkg/templates/   — Embedded templates (go:embed + text/template). One template per output artifact. (planned)
pkg/coolify/     — Coolify API client + file generators for Coolify resources. (planned)
pkg/generate/    — Orchestrates template rendering from a Plan. (planned)
```

Library code lives in `pkg/*`. It must have zero dependency on CLI or GUI code.

## Architecture Rules

1. **Library-first.** All logic goes in `pkg/`. `cmd/berth-cli` is a thin wrapper that calls the library. Future GUI (Wails/Tauri TBD) will reuse the same `pkg/` without changes.
2. **Detect is read-only.** `detect.Scan(path)` never writes to disk. It only reads and returns `plan.Plan` + `Notes` explaining reasoning.
3. **Plan is the source of truth.** `plan.Plan` holds everything needed to render templates. Add fields there, not globals.
4. **Check before Generate.** Always run `Plan.Check()` and handle `Report.HasBlockingFailure()` / `HasFixable()` before writing files. Respect `--fix` and `--dry-run`.
5. **Templates are embedded.** Use `go:embed` with `text/template` under `pkg/templates/`. Keep templates small, composable, and parameterized by `Plan`.

## Detection (Current + Planned)

**Currently implemented:**
- `laravel/framework` presence (error if missing)
- PHP version from `composer.json` `require.php` constraint
- Package manager via lockfile (`package-lock.json` → npm, `bun.lock`/`bun.lockb` → bun)
- Wayfinder (`@laravel/vite-plugin-wayfinder` in package.json)
- SSR via `--ssr` in `package.json` scripts + validation of `config/inertia.php` `ssr.url`

**Scheduled / expand incrementally:**
- Octane, Horizon, Reverb, Scheduler detection
- Frontend framework / Vite / Inertia details
- Node version, DB/cache/queue drivers from `.env` / config
- Queue workers: flag `--workers` today ( `-1` = prompt, `0` = none), later auto-detect

**Coolify DB:** Prefer connecting to an existing database selected from Coolify over creating a new one. This requires Coolify API listing + user selection.

## CLI

Current command:

```
berth init [--path .] [--workers -1] [-v] [--fix] [--dry-run]
```

- `--path` — Laravel project root (must contain `composer.json`)
- `--workers` — queue worker count
- `-v` — print detection `Notes` (reasoning)
- `--fix` — auto-fix fixable `Check` failures
- `--dry-run` — preview fixes without writing

Future commands will follow same `berth <command> [flags]` pattern. Keep `main.go` dispatch simple.

## Templates & Generation

- Engine: `text/template` + `go:embed` in `pkg/templates/`
- Outputs (as needed per Plan): `Dockerfile`, `.dockerignore`, entrypoint script, nginx/supervisord configs, docker-compose / Coolify resource files
- Templates must be deterministic and idempotent. Never overwrite user files without explicit flag.
- Parameterize PHP version, package manager, SSR, Wayfinder, worker count.

## Coolify Integration (Hybrid)

- **Generate mode:** Always produce files Coolify can consume (no API call needed).
- **API mode:** When configured, call Coolify API to create/update resources (app, workers, connect existing DB/Redis). Keep API client isolated in `pkg/coolify/` so generation still works offline.
- Do not hardcode Coolify URLs/tokens. Take from config/env/flags.

## Conventions for Agents

- **Go:** `go 1.25`, `module github.com/chadsmith12/berth`
- **Format:** `gofmt` / `go fmt ./...`
- **Vet/Test:** `go vet ./...` and `go test ./...` must pass before finishing
- **No comments** unless asked. Match existing style: short funcs, explicit errors, no log spam.
- **No new dependencies** without checking `go.mod` and existing imports.
- **Fix pattern:** `Result.Fixable` + `Fix func() error` with `CanFix()` guard. Use for auto-fixable checks (see `pkg/plan/check.go` SSR example).
- **Notes:** Use `Plan.Note()` to record detection reasoning — surfaced with `-v`.

## Workflow for Changes

1. Read `pkg/plan/plan.go` and `pkg/detect/detect.go` first — understand Plan shape.
2. Add detection in `pkg/detect/`, types in `pkg/plan/`, validation in `pkg/plan/check.go`.
3. Add templates in `pkg/templates/` and rendering in `pkg/generate/` (when it exists).
4. Wire CLI flags in `cmd/berth-cli/main.go` — keep it thin.
5. Run `go fmt ./... && go vet ./... && go test ./...`

## What Not To Do

- Don't put business logic in `cmd/` — it belongs in `pkg/`.
- Don't make `pkg/` depend on CLI flags or GUI frameworks.
- Don't write files during `detect`/`check` — only during `generate`/`fix`.
- Don't assume `package.json` exists — Laravel projects may be PHP-only.
- Don't guess Coolify API shapes — confirm with user or leave TODO.

## Open Questions for Maintainer

- Which Go template helpers are preferred?
- Where do Coolify credentials/config live (env, `~/.config/berth/`, project file)?
- Exact list of generated files for MVP?
