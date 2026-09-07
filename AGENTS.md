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
pkg/commands/    — Command orchestration + view types (the public output contract). The CLI/GUI seam. `commands.Open` builds the per-invocation Session (placement, token, client, env registry) — the single resolution point.
pkg/cli/         — Command tree, global flags (--env / --json / -c), terminal detection. No business logic.
pkg/input/       — Interactive prompts (stdin/stderr). The CLI's decision supplier.
pkg/output/      — The single render call: text views, JSON envelope, stable error codes, exit codes 0-4.
pkg/detect/      — Filesystem scanning. Reads composer.json, package.json, lockfiles, configs. Returns Plan.
pkg/plan/        — Core domain types (Plan, Project, Report, Result). Plan.Check() validates. Imports nothing.
pkg/templates/   — Embedded templates (go:embed + text/template). One template per output artifact.
pkg/config/      — Credentials (0600), repo registry (.config/berth.json | berth.json), user defaults (config.json 0644), placement resolution.
pkg/coolify/     — Coolify API client (token→team, typed ApiError, typed resource endpoints probed against the test instance).
pkg/generate/    — Orchestrates template rendering from a Plan. Compose output is docker-compose.<env>.yml.
pkg/launch/      — Launch orchestration: idempotent find-or-create steps (project/env/server/deploy key/database/application), plus deploy-key generation (in-process ed25519 OpenSSH), compose parsing (yaml.v3) and git-URL normalization. Inputs are plain data; nothing prompts.
pkg/deploy/      — Deployment follow loop: polls GET /deployments/{uuid}, diffs optional logs into events, honors timeout + context. The CLI's stdout loop and a GUI consume the same channel.
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
- PHP version from `composer.json` `require.php` constraint, **raised to the minimum that satisfies `composer.lock`** (max lower bound across locked non-dev packages' `require.php` + platform.php; alternation takes the lowest branch, conjunction the highest). The json floor alone produced a broken deploy (lock had symfony packages needing >= 8.4.1 while the image built 8.3).
- Horizon (`laravel/horizon` in composer.json → single horizon service, redis queue/cache)
- Package manager via lockfile (`package-lock.json` → npm, `bun.lock`/`bun.lockb` → bun)
- Wayfinder (`@laravel/vite-plugin-wayfinder` in package.json)
- SSR via `--ssr` in `package.json` scripts (script name recorded in `Plan.Project.SsrScript`) + validation of `config/inertia.php` `ssr.url`

**Scheduled / expand incrementally:**

- Octane, Reverb detection
- Frontend framework / Vite / Inertia details
- Node version, DB/cache/queue drivers from `.env` / config
- Queue workers: flag `--workers` today ( `-1` = prompt, `0` = none), later auto-detect

**Coolify DB:** Prefer connecting to an existing database selected from Coolify over creating a new one. This requires Coolify API listing + user selection.

## CLI

Binary is `berth-cli` (we are not renaming it to `berth` for now). Current commands:

```
berth generate [--path .] [--workers -1] [--scheduler] [--port N] [-v] [--fix] [--dry-run] [--force]
```

- `--path` — Laravel project root (must contain `composer.json`)
- `--workers` — queue worker count (ignored when Horizon is detected — horizon runs as a single service)
- `--scheduler` — run a scheduler container; asked when omitted (prompt interactive, usage error non-interactive)
- `--port` — port the app container serves on; asked when omitted
- `-v` — print detection `Notes` (reasoning)
- `--fix` — auto-fix fixable `Check` failures
- `--dry-run` — preview fixes, diffs and writes without touching disk
- `--force` — overwrite existing generated files that differ

`generate` is stateless: it does not call `commands.Open` — no config, no network. File semantics: identical files need no `--force`; differing files are shown as a unified diff and refused without it. `APP_ENV` follows `--env`; compose output is `docker-compose.<env>.yml`. The domain-ports check belongs to `launch` (generate never sees a domain).

```
berth launch [--path .] [--project <name>] [--server <uuid>] [--branch <b>]
             [--domain <url>] [--database <uuid>|none] [--db-name <name>]
             [--redis <uuid>] [--key <uuid>] [--uuid <app-uuid>] [--force]
```

- `commands.Open(ctx, {VerifyTeam: true, RepoRoot: path})` — a mis-scoped token is refused before anything is created; the registry is read/recorded in the **project directory** (`--path`), never the working directory.
- Preconditions: `docker-compose.<env>.yml` must exist (launch never runs generate); the compose file yields the app port and the Horizon flag (then `--redis` is required).
- Decisions are ask-or-flag, never silent: project (always, even a single one), server (skipped when the team has exactly one), database (`none` accepted), db-name (strict: resource's name offered interactively, required non-interactively), deploy key, branch, domain. Non-interactive misses are usage errors naming the flag and listing options.
- Domain: the port comes from the compose file — portless domains get it appended, explicit ports are cross-checked and refused on mismatch.
- DB/REDIS host = the resource **uuid** (Coolify names containers by uuid on the shared network). A `--db-name` differing from the resource's `postgres_db` is injected with a must-exist warning — the resource is never mutated.
- Deploy keys: creating one does NOT stop the launch (creation never touches the git host) — the public key + paste instructions land in the end checklist. App creation is the only non-idempotent step — crash between create and record → adopt mode `launch --uuid <app-uuid>`.
- End checklist: created deploy key, database notices, git state of the deploy branch (uncommitted files + unpushed commits), then `berth deploy`.
- `pkg/launch.Execute(client, Inputs)` does the find-or-create orchestration (project/env/server/key/db/app + APP_KEY-once + DB/REDIS env injection); records `environments[name].uuid` in the repo config, refusing an already-linked env without `--force`.

```
berth deploy [--uuid <app>] [--deployment <uuid>] [--detach] [--timeout 20m]
```

- `commands.Open(ctx, {VerifyTeam: true})`; app uuid resolved `--uuid` → `BERTH_UUID` → repo registry (`environments[<env>].uuid`); missing all is a usage error naming all three. Deploy never creates anything.
- Follows by polling (`pkg/deploy.Follow`): status transitions + **visible log lines** — the deployment record's `logs` field is a JSON array whose `hidden: true` entries are Coolify's internal commands (debug noise, filtered; probed). On failure: last 20 visible lines + the Coolify UI deployment URL, stderr + exit 1 (tail skipped when lines were already streamed). `--ci` deferred until a real build-completion signal exists.
- `--detach` triggers and prints the deployment uuid; `--deployment <uuid>` attaches to an existing one without triggering. `--json` = one final envelope, never an event stream.

```
berth status [--uuid <app>]
berth logs   [--uuid <app>]
```

- Read-only views of the linked application; both resolve the uuid the same way as deploy (`--uuid` → `BERTH_UUID` → repo registry) and write nothing anywhere. Shared helpers in `pkg/commands/apps.go` (`resolveAppUUID`, `classifyAPIError`, `sessionEnv`, `appState`).
- `status`: app detail (name, `<state>:<health>` status, branch, repo, domains from `docker_compose_domains` with Fqdn fallback, compose location) + newest record from `GET /deployments/applications/{uuid}` (returns `{count, deployments}`, newest first — shapes probed).
- `logs`: flagless; prints `GET /applications/{uuid}/logs`'s `logs` string verbatim ("no logs yet" when empty). When Coolify refuses and the app is not `running*`, the error explains the state and names `berth deploy`.

Global flags: `--env <name>` (default `production`), `--profile <name>`, `--json`, `-c <path>`. There is **no `--yes`**: a value that cannot be inferred is asked for (terminal) or a usage error naming the flag (non-interactive).

### Output conventions (design doc §9)

- A command does all its work, builds one view value, and hands it to `output.Emit` — the single render call. Commands never print mid-work and never branch on format.
- JSON envelope: `{"success", "command", "data", "error": {"code", "message"}}`. Stable error codes: `usage_error`, `auth_error`, `checks_failed`, `operation_failed`, `internal_error`.
- Exit codes: 0 success, 1 failure, 2 usage, 3 auth, 4 checks failed (constants in `pkg/output`).

Auth commands:

```
berth auth login [--url <u>] [--token-stdin] [--as <name>] [--default]
                                               store a token as a named profile; team auto-detected via GET /teams/current
berth auth list                                profiles (default marked) + stored tokens (truncated)
berth auth default [<name>]                    show profiles, or set the default without re-entering a token
berth auth whoami [--url <u>]                  team the token acts as; warns on drift from the resolved identity
berth auth logout [--url <u>] [--team <id>]    remove a stored token
```

- Credentials: `~/.config/berth/credentials.json` (0600), keyed by instance URL + team id
- Profiles: `~/.config/berth/config.json` (0644) — named instance+team pairs + `"default"` pointer; tokens stay in credentials.json
- Login names the profile from the team slug (prompted with suggestion); `--as` skips the prompt; `--default` sets the pointer; the **first profile created becomes the default**; plain login never changes the default; a name already pointing at a different identity is refused (pass `--as`)
- URL: `--url` → `BERTH_URL` → repo config → user default → prompt (login) or error naming every place (whoami/logout)
- Token: `BERTH_TOKEN` overrides stored credentials (pipeline mode); never a flag — prompt, `--token-stdin`, or stored
- Token format validated as `<id>|<secret>` before any request
- Team drift: acting commands must call `coolify.Client.VerifyTeam(ctx, teamID)` before mutating — a mis-scoped token (404-across-teams) is caught up front. `whoami` warns instead of refusing (it is the diagnostic tool).

## Configuration

- **Repository:** `.config/berth.json` (preferred) or `berth.json` at repo root — both present is an error. Committed. Holds `environments` (name → application uuid) and an optional `coolify` block (`url`, `team: {id, name}`, `project`).
- **User:** `~/.config/berth/config.json` (0644) holds profiles (named instance+team identities) and the `"default"` pointer; `~/.config/berth/credentials.json` (0600) holds tokens. See auth section for login/default semantics.
- **Placement resolution**: the identity (instance+team) is picked first — `--profile` / `BERTH_PROFILE` → repo `coolify` block → default profile. Then per-run field overrides above any identity: `--url` / `--team` / `--project` and `BERTH_URL` / `BERTH_TEAM` / `BERTH_PROJECT`. `BERTH_TEAM` is a team id (integer). An unresolved value is an error naming every place it could be set. Implemented in `config.Resolve(ResolveOptions)`; registry accessors: `FindRepoConfig`, `LoadRepoConfig`, `SaveRepoConfig`, `RepoConfig.EnvironmentFor`; profile helpers in `pkg/config/profiles.go` (`ProfileSlug`, `UpsertProfile`, `ProfileConflict`, `SetDefault`).

Future commands will follow same `berth-cli <command> [flags]` pattern. Keep `main.go` dispatch simple.

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
- **No comments besides public apis** unless asked. Match existing style: short funcs, explicit errors, no log spam. It is okay to use comments to document public APIs.
- **No new dependencies** without checking `go.mod` and existing imports.
- **New API commands:** call `commands.Open(ctx, SessionOptions{...})` and work from the Session — never re-implement placement/token resolution. `VerifyTeam: true` for commands that act (reads included); `PlacementOnly` for token-free commands (logout); `OptionalTeam` only for diagnostics (whoami).
- **Fix pattern:** `Result.Fixable` + `Fix func() error` with `CanFix()` guard. Use for auto-fixable checks (see `pkg/plan/check.go` SSR example).
- **Notes:** Use `Plan.Note()` to record detection reasoning — surfaced with `-v`.
- Don't create redundant tests. Not every command or argument needs tests.

## Workflow for Changes

1. Read `pkg/plan/plan.go` and `pkg/detect/detect.go` first — understand Plan shape.
2. Add detection in `pkg/detect/`, types in `pkg/plan/`, validation in `pkg/plan/check.go`.
3. Add templates in `pkg/templates/` and rendering in `pkg/generate/`.
4. Wire CLI flags and view types in `pkg/commands/` — `cmd/berth-cli/main.go` stays a thin dispatch.
5. Run `go fmt ./... && go vet ./... && go test ./...`

## Test Apps

`test-apps/` holds committed **buildable stub fixtures** (php-only, npm-vite, bun-ssr-wayfinder, horizon, horizon-ssr): real composer/npm/bun locks, stub `artisan`, build scripts satisfying the structural contracts. `pkg/commands/e2e_test.go` copies each to a temp dir, runs the real `generate` command in-process, and byte-compares against `golden/`: `go test ./pkg/commands -run TestGenerateE2E -update` regenerates goldens after template changes — golden diffs are the review record for template work.

**`test-apps/smoke-build.sh` is the template-validity guarantee**: it copies each fixture, runs the real `generate`, and `docker build --target runtime` the result (the same build Coolify runs). Run it before pushing any template change — goldens cannot catch build-breaking bugs (the `COPY --from` variable failure and the composer-lock/PHP mismatch were both invisible to them). Skips cleanly without docker.

`test-apps/real/` (gitignored) is for real Laravel apps via `scaffold.sh` → `docker build` e2e; it is manual, needs php+composer+network.

## What Not To Do

- Don't put business logic in `cmd/` — it belongs in `pkg/`.
- Don't make `pkg/` depend on CLI flags or GUI frameworks.
- Don't write files during `detect`/`check` — only during `generate`/`fix`.
- Don't assume `package.json` exists — Laravel projects may be PHP-only.
- Don't guess Coolify API shapes — confirm with user or leave TODO.

## What to use/look a at

- `docs/cli-design.md` has current design for the CLI for what we are curently modeling off of. Follow this but it is okay to suggest improvements if seen but only after proper research.
- Use official Coolify API Documentation - https://coolify.io/docs/api-reference/authorization
