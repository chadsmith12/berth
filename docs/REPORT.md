# Berth Codebase Simplification Audit — Canonical Report (FINAL)

STATUS: complete · repo untouched (read-only audit) · validated 2026-09-06
Go 1.25 · module `github.com/chadsmith12/berth` · ~10.8k LOC Go · 19 subsystems reviewed

## 1. Subsystem inventory (coverage contract — final)

| ID  | Subsystem                        | Ownership boundary                           | Key files                                 | Outcome                                      |
| --- | -------------------------------- | -------------------------------------------- | ----------------------------------------- | -------------------------------------------- |
| S1  | CLI dispatch & command tree      | binary entry + flag/command tree             | `cmd/berth-cli/main.go`, `pkg/cli/*`      | 2 recommend                                  |
| S2  | Session & shared command helpers | session construction + cross-command helpers | `pkg/commands/{session,apps,root}.go`     | 2 recommend                                  |
| S3  | generate command                 | generate orchestration                       | `pkg/commands/generate.go`                | 2 recommend                                  |
| S4  | launch command                   | launch orchestration                         | `pkg/commands/launch.go`                  | 2 recommend                                  |
| S5  | deploy command                   | deploy orchestration                         | `pkg/commands/deploy.go`                  | 2 recommend                                  |
| S6  | auth command                     | auth subcommands                             | `pkg/commands/auth.go`                    | 2 recommend                                  |
| S7  | status & logs commands           | read-only app views                          | `pkg/commands/{status,logs}.go`           | skip (validated)                             |
| S8  | Interactive input                | prompt primitives                            | `pkg/input/*`                             | 1 recommend, 1 skip                          |
| S9  | Output envelope                  | single render call                           | `pkg/output/*`                            | 1 recommend (low), 4 skips                   |
| S10 | Detection                        | read-only scan → Plan                        | `pkg/detect/*`                            | 2 recommend                                  |
| S11 | Plan domain & checks             | domain types + validation                    | `pkg/plan/*`                              | 2 recommend                                  |
| S12 | Config                           | credentials/profiles/repo/resolve            | `pkg/config/*`                            | 2 recommend                                  |
| S13 | Coolify API client               | HTTP client                                  | `pkg/coolify/*`                           | 2 recommend (one low)                        |
| S14 | Generation & diff                | render orchestration + write semantics       | `pkg/generate/*`                          | 2 recommend                                  |
| S15 | Templates                        | embedded templates + render entrypoints      | `pkg/templates/*`                         | 2 recommend                                  |
| S16 | Launch library                   | find-or-create, compose parse, git URL, keys | `pkg/launch/*`                            | skip (validated) + 1 coordinator-added (low) |
| S17 | Deploy follow                    | deployment poll loop                         | `pkg/deploy/*`                            | 2 recommend                                  |
| S18 | Test fixtures & e2e              | fixtures + goldens + smoke build             | `test-apps/*`, `pkg/commands/e2e_test.go` | 2 recommend                                  |
| S19 | Scripts & docs                   | probe script + docs                          | `scripts/*`, `docs/*`, AGENTS.md          | 2 recommend                                  |

Coverage verified: every directory of the repo (`cmd/`, `pkg/` × 13, `test-apps/`, `scripts/`, `docs/`, `go.mod`) is owned by exactly one row. Two worker re-dispatches occurred (S14, S4 returned empty once each); all completed on retry.

## 2. Confirmed opportunities (31 accepted, condensed with all required fields)

Ordering below = final priority. "Effort" is small (≤1 file+tests) / medium (2–6 files, mechanical) / medium+.

### Tier 1 — bug-class, tiny effort, low blast radius (best first slices)

**A. S10#1 — PHP floor parser duplicates `minSatisfyingPhp` with divergent semantics** · recommend · HIGH confidence · effort small

- Evidence: `pkg/detect/detect.go:253-266` (`readPhpVersion`, first-match regex `(\d+)\.(\d+)`) vs `detect.go:171-204` (`minSatisfyingPhp`: alternation→lowest, conjunction→highest, skips `<`/`!`). Only the composer.json path (detect.go:68) uses the regex.
- Complexity: for `"php": "<8.5"` the regex returns the **upper bound 8.5 as the image floor** — the exact broken-deploy class AGENTS.md documents the lock-floor work was done to prevent. `minSatisfyingPhp` is table-tested; `readPhpVersion` has zero direct tests.
- Proposal: delete the regex; `readPhpVersion` delegates to `minSatisfyingPhp`; empty result keeps the existing error (or falls to the default path — pick deliberately).
- Scope: `pkg/detect/detect.go` only. Risks: behavior changes only where first-match ≠ floor (upper-bound constraints, currently wrong); `"php": "*"` today hard-errors — decide error vs default. Validation: existing `TestMinSatisfyingPhp`, `TestScanKeepsJsonVersionWhenHigher`, `TestScanRaisesPhpFromComposerLock`; add a `<`-first constraint test.

**B. S5#1 — `--json` mode leaks the deploy event stream into stdout** · recommend · HIGH confidence · effort small

- Evidence: `pkg/commands/deploy.go:134-146` prints status dots/log lines to `ctx.Stdout` unconditionally; `pkg/output/output.go:103-120` writes the envelope to the same stream. Contract (AGENTS.md deploy section): "`--json` = one final envelope, never an event stream."
- Complexity: JSON stdout = `● in_progress` + raw logs + envelope. Latent: `TestDeployJSONFinalEnvelope` uses a single-step script (`deploy_test.go:27`) that never emits stream events; multi-step scripts only run in text mode.
- Proposal: `stream := ctx.Stdout; if ctx.Globals.JSON { stream = io.Discard }`; derive `Streamed` from `stream == ctx.Stdout`. Scope: `pkg/commands/deploy.go` only (~4 lines). Risks: none for documented behavior. Validation: add a multi-step JSON test asserting exactly one JSON document + `log_tail` present.

**C. S17#2 — timeout classification races select arbitration; deadline can surface as `KindError{deadline exceeded}`** · recommend · HIGH confidence · effort small

- Evidence: `pkg/deploy/deploy.go:110-119` selects on `watchCtx.Done()` vs `tick.C` — both ready after the deadline; `:121-128` poll on the dead ctx fails with `DeadlineExceeded`, counted as a poll failure (burns the 3-retry budget) and can emit `KindError` instead of `KindFailed "timed out"`. Consumer (`pkg/commands/deploy.go:147-149`) short-circuits `KindError` to stderr — user loses the designed timeout view. `TestFollowTimeout` (deploy_test.go:173) is ~94% reliable, not deterministic.
- Proposal: extract `giveUp()` owning the terminal timeout transition; call it from the Done case **and** as first check in the poll-error branch (`if watchCtx.Err() != nil { giveUp(); return }`). Optionally retype `terminal` to take `status string` (removes fabricated records at deploy.go:103,114) and fix the false "last event is always terminal" doc at :53-55.
- Scope: `pkg/deploy/deploy.go` only (~10-15 lines). Risks: low — happy paths untouched (`watchCtx.Err() == nil`). Validation: existing tests pass; add a deterministic race test (client returns `ctx.Err()`, Timeout == PollInterval → always `KindFailed`).

**D. S4#2 — unreachable branch in linked/adopt guard; orphaned `adoptView`** · recommend · HIGH confidence · effort small

- Evidence: `pkg/commands/launch.go:144-157`. `linkedUUID` (`:317-326`) guarantees non-empty; the guard requires `in.uuid == "" || in.uuid != existing` before testing `in.uuid == existing` — contradiction, so `return adoptView(existing)` (`:147`, sole call site of `adoptView` `:310-312`) is dead. The intended fast-path adopt never runs.
- Proposal: flatten to one condition: refuse iff `ok && !force && (in.uuid == "" || in.uuid != existing)`; then `if in.uuid != "" { adopt }`. Behavior-identical across all six cases. Scope: `pkg/commands/launch.go` only. Validation: existing `TestLaunchHappyPath`, `TestLaunchRefusesRelinkWithoutForce`; add re-adopt with the linked uuid (idempotent success) — currently untested.

**E. S12#1 — `Overrides.TeamID int` 0-sentinel collides with real team 0** · recommend · HIGH confidence · effort small

- Evidence: `pkg/config/resolve.go:12-17` ("TeamID 0 means unset"), `:96-110` (`case opts.Overrides.TeamID != 0`); fed from `pkg/commands/session.go:62-68`. Team 0 is a real, tested id (root team: `credentials_test.go:15`, `resolve_test.go:166-177`, `session_test.go:34-40`). `auth.go:437-439` deliberately passes `--team 0` through — config drops it. `BERTH_TEAM` is parsed twice (`:79-83`, `:100-102`). Validity regimes disagree: `repo.go:165-167` rejects team 0, `profiles.go:97-119` allows it.
- Complexity: `berth auth logout --team 0` with several stored tokens can delete **another team's token**; acting commands with `--team 0` silently use the default team.
- Proposal: `Overrides.TeamID *int` (nil = unset); internal `teamFound` dance may stay. Scope: `pkg/config/resolve.go` + forced mechanical `pkg/commands/session.go:62-68` + test literals. Risks: none — only `--team 0` changes (currently wrong). Validation: add override-of-0 case + logout `--team 0` removes only team 0's token.

**F. S6#1 — logout targets the team via `teamID == 0` sentinel despite `Placement.Team` being `*TeamRef`** · recommend · HIGH confidence · effort small

- Evidence: `pkg/commands/auth.go:455-470` (`teamID, teamName := 0, ""`; `if teamID == 0` → fallback switch); `:393` (`v.TeamName != "" || v.TeamID > 0` excludes team 0 again); `:379` (`team_id,omitempty` can't express 0). Whoami already models optional team as a pointer (`:347` `*TeamView`).
- Complexity: default profile on team 0 + two tokens → "several tokens are stored… pass --team" error even though a team is configured; `--team 0` can't rescue (dropped upstream, see E).
- Proposal: branch on `sess.Placement.Team == nil`; view becomes `Team *TeamView json:"team,omitempty"` (or minimal variant: fix only the branch condition). Scope: `pkg/commands/auth.go` only. Risks: JSON shape change (do now, pre-GUI) or minimal variant avoids it. Validation: add team-0 + two-token removal test.

**G. S8#1 — hidden `*bufio.Reader` strands pasted input across Terminals** · recommend · HIGH confidence · effort small

- Evidence: `pkg/input/input.go:17-23,30,39,50-56` — `Terminal.reader` may consume past the returned line; `PromptPassword`'s TTY path reads raw from the fd. `auth.go:131,230-231` builds two Terminals over one stdin per `auth login`; `generate.go:375,390,411` builds three.
- Complexity: pasting `url\ntoken\n` at the URL prompt strands the token line in Terminal #1's buffer; Terminal #2's `term.ReadPassword` blocks; the secret is silently lost. Same for generate type-ahead.
- Proposal: ~12-line byte-wise `readLine()` over `t.In` (the discipline `term.readPasswordLine` uses); delete the `reader` field. Note: `bufio.NewReaderSize(in,1)` is insufficient (clamps to 16). Scope: `pkg/input/input.go` only, no API change. Validation: all 21 existing tests; add two-Terminals-over-one-reader no-over-read test.

### Tier 2 — determinism & correctness-adjacent, small effort

**H. S10#2 — SSR script selection scans a Go map (nondeterministic output)** · recommend · HIGH confidence · effort small

- Evidence: `pkg/detect/detect.go:40-48` — first map-iteration match wins; two matching scripts (canonical Inertia setup: `build` containing `&& vite build --ssr` + `build:ssr`) yield run-to-run different `SsrScript`; rendered verbatim into `Dockerfile.tmpl:88-96`. Violates the repo's own determinism rule. No multi-match test; goldens don't exercise SSR at all (see S18#1).
- Proposal: deterministic rule — prefer names containing "ssr", lexicographic tie-break; ~8 lines + test. Prerequisite for S18#1's golden regen (else goldens flake).

**I. S18#1 — "ssr" fixtures never exercised SSR; horizon-ssr golden is byte-identical to horizon's** · recommend · HIGH confidence · effort small

- Evidence: `test-apps/{horizon-ssr,bun-ssr-wayfinder}/package.json:7-10` — no script **value** contains `--ssr`, so `detect.go:40-48` never fires; `diff horizon/golden/docker-compose.production.yml horizon-ssr/golden/...` → identical (verified). The entire SSR template surface (`docker-compose.yml.tmpl:36-39,53-57,68-85`; `Dockerfile.tmpl:88-96,121-124`; `entrypoint.sh.tmpl:4,27`) has zero golden/docker coverage while README claims it does.
- Proposal: add a `# stub for: vite build --ssr` marker to both fixtures' `build:ssr` (comment carries the `--ssr` token; stub stays buildable); regenerate goldens (`-update`); run `smoke-build.sh` once (docker). Scope: 2 fixtures + 8 golden files. Risks: large-but-intended golden diff (that is the review record); smoke build must pass the `test -f bootstrap/ssr/app.js` guard.

**J. S4#1 — with `-c`, the linked-precheck reads one registry file but launch records into another** · recommend · HIGH confidence · effort small

- Evidence: `pkg/commands/session.go:53` honors `ctx.Globals.Config` via `LoadRepoFor(repoRoot, ctx.Globals.Config)`; `pkg/commands/launch.go:144` pre-checks `sess.Repo`; `recordEnvironmentUUID` (`:328-352`) re-discovers with `config.FindRepoConfig(root)` ignoring `-c`, defaulting to `.config/berth.json`. `FindRepoConfig` runs twice per launch.
- Complexity: `berth launch -c custom.json` leaves bookkeeping split across two files; next run's pre-check evaluates a stale snapshot.
- Proposal: resolve the path once (`-c` → else `FindRepoConfig` → else default), thread as a value into `recordEnvironmentUUID` (drop its private discovery). Scope: `pkg/commands/launch.go` only. Validation: existing happy-path/relink tests; add a `-c` launch test asserting the uuid lands in the `-c` file.

**K. S13#1 — env-var writes re-list all envs per key (N+1 round trips; split-brain snapshots)** · recommend · HIGH confidence · effort medium

- Evidence: `pkg/coolify/resources.go:264-275` — `SetEnvVar` lists `GET /applications/{uuid}/envs` on **every** call; `pkg/launch/launch.go:347-377` already listed the same endpoint for APP_KEY, then loops `SetEnvVar` per key → 7–10 identical GETs per launch on the critical path (30s-timeout client). Two different snapshots answer "does APP_KEY exist?" — the exact overwrite-existing-APP_KEY failure the launch comment forbids.
- Proposal: batch `SetEnvVars(ctx, uuid, map)` owning create-vs-update from one snapshot; launch passes the full `set` map; drop single `SetEnvVar` + `launch.Client.SetEnvVar` (`launch.go:29`). Scope: `pkg/coolify/resources.go`, `pkg/launch/launch.go`, fake at `launch_test.go:90-94`. Risks: low — write semantics unchanged; keep fail-fast + key-named wrap. Validation: existing idempotency tests; add one-GET-per-batch httptest assertion.

**L. S17#1 — log-diff state is a joined blob string; join→split round-trips every poll (O(n²) over a build)** · recommend · HIGH confidence · effort small

- Evidence: `pkg/deploy/deploy.go:88` (`lastBlob string`), `:93-95` (`blob()` join), `:138,141` (`VisibleLogs()` computed twice per poll), `:159-177` (`emitLogDelta` re-splits the whole previous blob; re-implements `emit`'s send-or-cancel select; the split is the identity on `VisibleLogs()` output).
- Proposal: store `lastLines []string`; emit `lines[len(lastLines):]` through the single `emit` closure; delete `blob`/`emitLogDelta`. Scope: `pkg/deploy/deploy.go` only. Validation: all follow tests pass unmodified; add identical-blob-consecutive-polls and empty→lines first-emission tests.

**M. S6#2 — login persists the token before its two abort points** · recommend · MEDIUM confidence · effort trivial

- Evidence: `pkg/commands/auth.go:156-173` saves credentials; the name prompt (`:184-194`) and conflict refusal (`:195-197`) are normal abort paths after the write.
- Proposal: move the credentials block after both abort points ("resolve everything, then commit"). Scope: one 12-line relocation. Risks: aborted login must re-verify on retry (negligible). Validation: existing login tests + assertion that conflict refusal leaves credentials unchanged.

### Tier 3 — structural simplifications, medium effort

**N. S2#1 — `PlacementOnly` builds a half-valid Session (nil Client); split placement from session** · recommend · HIGH confidence · effort small

- Evidence: `pkg/commands/session.go:38,87-90` (early return with `Client == nil`); `:104-108` VerifyTeam unreachable under PlacementOnly (silently-ignored invalid combo); `auth.go:436` must remember the undocumented `PlacementOnly+OptionalTeam` pairing; `session_test.go:216-218` asserts the invalid state as contract. Logout uses only `sess.Placement.URL` (`auth.go:453-478`).
- Proposal: `OpenPlacement(ctx, opts) (config.Placement, error)` extracted; `Open` calls it then proceeds; drop the boolean. Postcondition: `Open` always returns a total session. Scope: `session.go` + `auth.go` logout + retargeted test. Validation: existing session tests unchanged; `go vet`/`go test ./pkg/commands`.

**O. S11#1 — collapse `HasSsr bool` + `SsrScript string` into `SsrScript` + derived method** · recommend · HIGH confidence · effort medium (mechanical)

- Evidence: `pkg/plan/plan.go:33-34`; sole producer sets them atomically (`detect.go:89-94`); two defensive checks of the impossible state (`check.go:94-101`, `generate.go:49-51`) + a test that hand-constructs it (`detect_test.go:64-77`). 13 read sites; templates gate on `{{if .Project.HasSsr}}`.
- Proposal: delete the field; `func (p Project) HasSsr() bool { return p.SsrScript != "" }`. `text/template` resolves the method identically → **all 11 template sites and goldens byte-identical**. Delete both defensive checks and the hand-constructed test. Scope: plan.go, check.go + mechanical compile fixes (detect, detect_test, generate, generate_test, commands/generate.go:288). Risks: negligible. Validation: e2e goldens zero-diff is itself the check.

**P. S1#1 — `App` re-implements `Command`'s registry and help/unknown routing; copies already drifted** · recommend · HIGH confidence · effort medium

- Evidence: `pkg/cli/cli.go:29-37` vs `:61-66` (same map+order structure twice; byte-identical AddCommand/Commands at :47-59 vs :75-84); duplicated help/unknown routing (`:98-127` in `Execute` vs `:151-173` in `dispatch`); observable drift — `berth --help generate` errors "unknown command" while `berth generate --help` works (verified by code path); duplicate `AddCommand` desyncs map vs order (`:51-52,:76-77`); dead `isHelp` branch for parents (`:184-187`); dead exports `App.Version` and `Commands()`/`Command.Commands()` (zero callers — verified by grep).
- Proposal: `App` becomes a thin wrapper over a root `Command`; `Execute` keeps only app-level policy (nil writers, parseGlobals, zero-args→stderr+2 rule); all routing delegates to `root.dispatch`. One registry, one routing table; the `--help` asymmetry disappears.
- Scope: `pkg/cli/cli.go` (+ tests; no `pkg/commands` changes). Risks: root help format must be preserved (`cli_test.go:25,49`); bare `berth` stays stderr+2; deliberate fix: `--help` before command name now works. Validation: existing 20 cli tests + e2e tree; add `--help <cmd>` and duplicate-registration cases.

**Q. S14#1 — decide-then-apply split in `Generate`; dry-run invariant enforced only by auditing two branches** · recommend · MEDIUM-HIGH confidence · effort small

- Evidence: `pkg/generate/generate.go:69-116` — decision matrix nested with writes; `writeFile` at two sites; `DryRun` guard twice; six `FileResult` append sites; render error aborts mid-write (earlier files already on disk). Dry-run's would-write/would-change branches have **zero test coverage** (dry-run test only covers already-identical files, `generate_test.go:126-147`; no `--dry-run` case in commands tests). Dead state: `Result.DryRun` set at `:41`, never read (verified).
- Proposal: pure `decide(...)` → `fileDecision{status, diff, reason, write}` + one apply loop with one guard + one write site; render-all-before-write-any; delete `Result.DryRun`. Scope: `pkg/generate/generate.go`. Validation: existing tests + goldens; add the two untested dry-run branches.

**R. S2#2 — "default env = production" owned in three places; let `Open` normalize once** · recommend · MEDIUM confidence · effort small

- Evidence: `pkg/commands/apps.go:50-55` (`sessionEnv`), `pkg/commands/launch.go:129-132` (inline re-implementation — launch forgot the helper), `pkg/commands/generate.go:125` (third copy, out-of-Session); `session.go:87` stores `Env` verbatim-empty; launch.go:329-332 re-defaults an already-normalized `RepoRoot`. A new command reading `sess.Env` directly gets `""` → wrong compose filename/registry key.
- Proposal: `session.go:87` → `Env: firstNonEmpty(ctx.Globals.Env, "production")`; delete `sessionEnv` + inline copies; drop the dead RepoRoot re-default. Scope: 6 files, all within pkg/commands. Risks: `sess.Env` no longer expresses "unset" (that's `ctx.Globals.Env`'s job). Validation: existing tests exercise the default.

**S. S14#2 — `Options.Env` duplicates `Plan.Project.Env` (AGENTS.md rule 3: Plan is the source of truth)** · recommend · MEDIUM confidence · effort small

- Evidence: `pkg/generate/generate.go:22,52-56` (re-defaults `""→"production"`, overwrites plan field); `pkg/commands/generate.go:123-127,173` (caller defaults env, sets plan field, passes the same value again); both files default independently. `TestGenerateHorizonTopology` (generate_test.go:153) proves silent Options-wins precedence.
- Proposal: remove `Options.Env`; `Generate` reads `p.Project.Env` (keep the single `""→"production"` fallback or reject empty in validation). Scope: generate.go + one-line caller + one test. Validation: goldens (compose filename embeds env) byte-exact.

**T. S3#1 — three "was this flag provided?" conventions; two misreport invalid values as "missing flag"** · recommend · HIGH confidence · effort small

- Evidence: `pkg/commands/generate.go:72-82` — workers uses sentinel `-1`, scheduler uses a `(value, provided)` pair, port overloads `0`; `fs.Visit` (`:106-110`) only records scheduler. `--workers -5` → "missing --workers" (`:368-374`); `--port 0` → "missing --port" though `validPort` calls 0 invalid (`:398-417`); prompt promises `[0-5]` but no bound enforced (`:376` vs `:426-431`).
- Proposal: record provided-ness once via `fs.Visit` for all three; uniform `(value, provided)` resolvers — provided → validate, else ask/usage-error. Scope: `pkg/commands/generate.go` ~30 lines. Risks: explicit `--workers -1` becomes a usage error (today "ask"); `--port 0`/`--workers -5` get accurate range errors (same exit 2). Validation: existing missing-flag tests; add the two invalid-value cases.

**U. S3#2 — `FixView` encodes a 3-way outcome as three orthogonal fields; zero value renders "[fixed]"** · recommend · MEDIUM-HIGH confidence · effort small

- Evidence: `pkg/commands/generate.go:45-50` (`Applied/DryRun/Error` — 8 combos, 3 valid); `writeFixes` default branch prints `[fixed]` without checking `Applied` (`:237-246`); `anyApplied` + redundant `!in.dryRun` guard (`:160`); dry-run double-encoded per-fix and globally. No fix-path tests exist; no external consumer of the JSON shape.
- Proposal: `FixView{Name, Status "fixed"|"would-fix"|"failed", Error}` mirroring the existing `FileView.Status` convention. Scope: generate.go only, ~25 lines net-negative. Risks: JSON envelope shape change (pre-GUI, no goldens pin it). Validation: add two fix-path tests (dry-run, applied).

**V. S5#2 — terminal event reduced to Kind+Err; timeout narrative silently dropped** · recommend · HIGH confidence · effort trivial

- Evidence: `pkg/commands/deploy.go:147-149` reads only `Kind`/`Err` — `Event.Message` ("timed out after 20m0s", produced at `pkg/deploy/deploy.go:112-117`) is never referenced; failure text re-derived from a fresh record fetch (`:151-173`), so a timeout prints "✗ deployment in_progress" with no timeout indication; duplicated `Tail` assignment at `:172`.
- Proposal: lead the error with `terminal.Message` when set; delete the duplicate `Tail` line. Scope: ~3-5 lines. Validation: add a timeout-path test (script pinned non-terminal + `--timeout 100ms`).

**W. S15#1 — third bun/npm switch in Dockerfile; SSR error tail maintained in two copies** · recommend · HIGH confidence · effort small

- Evidence: `pkg/templates/templates/Dockerfile.tmpl` — `{{if eq .Project.PackageManager "bun"}}` at the binary COPY, deps-install, and SSR/build blocks; the SSR branch's 2-line `test -f … || (echo ERROR…)` tail is byte-identical between bun/npm (verified at lines ~88-96). Also latent: `HasSsr=true` + `PackageManager=""` renders `npm run` with no npm installed (guard belongs in plan/check — outside this boundary).
- Proposal: `jsrun` funcMap entry (`bun run`/`npm run`); collapse lines 88-105 to 7 flat lines; the tail exists once. Goldens unaffected (SSR branch never rendered by fixtures; build lines byte-identical via `jsrun`). Scope: templates.go + Dockerfile.tmpl. Validation: golden `-update` zero-diff; add a RenderDockerfile HasSsr unit test.

**X. S16-cc — compose filename convention single-sourced** (coordinator-added; flagged by S14 as out-of-boundary, assigned to S16 as contract owner) · recommend · MEDIUM confidence · effort trivial

- Evidence: `"docker-compose." + env + ".yml"` written at `pkg/generate/generate.go:66`, `pkg/commands/launch.go:135`, `:223`; contract documented on `pkg/launch/launch.go:48` (`EnvFileName`) but no helper exists. Launch's lookup must match generate's output name or the command dead-ends.
- Proposal: `launch.ComposeFileName(env)` (or plan-level helper) used by all three. Validation: existing e2e + launch tests pin the name.

### Tier 4 — lower priority / borderline / preventive

**Y. S15#2 — precompute the sidecar-service list (horizon/worker-N/scheduler)** · recommend · MEDIUM confidence · effort medium

- Evidence: `pkg/templates/templates/docker-compose.yml.tmpl:88-143` — three ~14-line blocks sharing 12 lines verbatim; the horizon-wins rule encoded by if/else ordering + a second time in the header comment + structurally. Note: scheduler has `deploy: replicas: 1`, horizon/workers don't (intent vs drift undecidable — resolve while consolidating).
- Proposal: unexported view struct in templates.go building the sidecar list; template ranges over it. Scope: templates.go + compose tmpl. Validation: golden regen (horizon + workers variants both exercised; scheduler is NOT exercised by fixtures — add a unit render).

**Z. S11#2 — `Workers int` cannot express "unresolved"; `checkWorkers` never fails** · recommend · MEDIUM confidence · effort small

- Evidence: `pkg/plan/plan.go:36` (0 = explicit none, but fresh `Scan` leaves 0); sentinel `-1` owned by the CLI (`commands/generate.go:93,368-381`); `check.go:63-80` always returns OK/Info; the only real guard is `generate.go:46-48` (`Workers < 0`) with the flag name baked into a library error; `seq(-1)` would panic in templates but for that guard.
- Proposal: `plan.WorkersUnset = -1` constant; `checkWorkers` returns a blocking failure when unset and `!HasHorizon`; `detect.Scan` initializes `Workers = WorkersUnset`. Scope: plan.go, check.go + one-line detect init. Risks: contract change for library callers calling `Check()` on raw scans (intended); CLI flow resolves before `Check()` so no CLI change. Validation: one hand-built-plan Check test.

**AA. S18#2 — variant decision matrix duplicated across e2e_test.go and smoke-build.sh (silent value drift)** · recommend · MEDIUM confidence · effort small

- Evidence: `pkg/commands/e2e_test.go:25-31` vs `test-apps/smoke-build.sh:11-17` — byte-identical decisions in two tables + variant list hardcoded a third time (`smoke-build.sh:25`) + README a fourth; nothing compares them; value drift breaks the "docker proves they work" guarantee silently. Stale comment referencing `defaultArgs` at `e2e_test.go:22-24`.
- Proposal: shared `test-apps/variants.tsv` read by both. Validation: e2e run + `bash -n` + one smoke run.

**AB. S12#2 — repo-config loading duplicated (`LoadRepoFor` vs `loadRepoForResolve`); silent `Repo`-beats-`ConfigPath` ambiguity** · recommend · MEDIUM confidence · effort small

- Evidence: `pkg/config/repo.go:74-97` vs `pkg/config/resolve.go:127-153` (+ `repoDescFor` `:155-160`) — parallel three-way loads; `session.go:53,77-82` passes both `Repo` and `ConfigPath` that must agree, enforced only by caller discipline.
- Proposal: `LoadRepoFor` returns `(cfg, path, err)`; resolve-side delegates; `ResolveOptions` carries the (Repo, RepoPath) pair. Scope: repo.go, resolve.go, session.go, one test. Validation: error-text tests (`resolve_test.go:274`) stay green.

**AC. S1#2 — global-flag knowledge in three uncoordinated places; `parseGlobals` repeats one pattern 4×** · recommend · MEDIUM confidence (borderline) · effort small

- Evidence: `pkg/cli/cli.go:221-252` (parse switch), `:103` ("was any global set" re-enumeration), `:272-277` (hardcoded help text); dead guard `a == "--json" || HasPrefix "--json="` in default branch (`:254`, unreachable — verified); undocumented `--json=` ⇒ true; prefix guard misattributes unknown `--env*`-prefixed flags as global errors (latent).
- Proposal: one ordered `globalFlag` table (name, usage, setter) + generic loop returning `seen`; usage check and help text derive from it. Scope: cli.go only. Validation: existing globals tests + `--json=`/`--json false`/`--envx` cases.

**AD. S9#1 — text mode silently drops non-TextView data** · recommend (preventive) · HIGH confidence · effort trivial

- Evidence: `pkg/output/output.go:121-125` — non-nil data failing the `TextView` assertion renders nothing (violates design-doc §9 "printed but not modelled is a bug"); the gap hides because all command tests run in JSON mode and e2e asserts only exit codes.
- Proposal: one-line stderr diagnostic naming `%T` in the else branch. Scope: output.go + one test. Risks: none for the 10 current call sites (all pass TextViews — verified).

**AE. S13#2 — four verbatim `{"uuid"}` create-decode copies** · recommend (lowest) · HIGH confidence · effort trivial

- Evidence: `pkg/coolify/resources.go:38-44, 58-65, 103-110, 179-185` — identical anonymous struct + post + return (the file already handles irregular shapes — `DeployApplication` array-wrap — so this is a real axis of variation). Verified.
- Proposal: `postForUUID(ctx, path, body)` helper; four methods become one-liners. Validation: existing launch httptest coverage.

**AF. S19#1 — AGENTS.md restates the design doc's behavioral contract; copies already diverged** · recommend · HIGH confidence · effort small

- Evidence: AGENTS.md:130-153,155-159,81-128,196-202 restate `docs/cli-design.md` §9/§auth/§config/§commands/test-apps. Verified drift: AGENTS.md:82 lists `[-v]` for generate, `docs/cli-design.md:336` omits it (the flag exists, `generate.go:96`).
- Proposal: AGENTS.md keeps agent conventions + pointers; collapse restated blocks (line-by-line pass first — several facts live only in AGENTS.md, e.g. `--path`-relative registry).

**AG. S19#2 — "Detection (Current + Planned)" partition is stale: Node version detection is implemented** · recommend · HIGH confidence · effort trivial

- Evidence: AGENTS.md:72 lists Node version under Scheduled; but `detect.go:88,96,269-288` implements it (default "22" + Note), rendered in generate output and templates, committed in goldens. AGENTS.md:168's parameterization list also omits it. Corroborates AF.
- Proposal: move Node version to "Currently implemented"; add to the parameterization list. No code impact.

## 3. Explicit skip decisions (completed coverage)

- **S7 — status & logs: SKIP (high confidence).** Status parsing already a shared helper (`apps.go:43-46` `appState`, 2 call sites); runStatus/runLogs scaffolding is 3 lines each by design; single-field input structs trivial; logs fallback branch linear and local. No invalid states.
- **S16 — launch library: SKIP (high confidence) for its two candidate patterns.** Find-or-create list-then-scan is a few lines each in boring shape; empty-string sentinels each carry exactly one meaning; postgres/redis env-var blocks differ meaningfully; the double-fetch across the launch↔commands boundary is an interface tradeoff, not simplifiable without new type churn. One non-idempotent step (`CreateApplication`) is the deliberate adopt-mode seam. (The coordinator added one low-priority cross-cutting finding to S16's account: X, compose-filename single-sourcing.)
- **S8#2 retry-loop/read-line dedup: SKIP.** ~20 lines, loops differ in label/feedback/parse; a callback generic is no simpler.
- **S11 near-misses: SKIP** — `Result{OK,Severity}` redundancy; per-call `regexp.MustCompile` in check.go; `Plan.Check()` doing I/O; `SupportedPackageManagers` placement.
- **S10 near-misses: SKIP** — `HasSsr` union (superseded by O's method approach); PhpVersion `""`-sentinel chain; nodeVersion regex micro-cleanup.
- **S13 near-misses: SKIP** — status-string relocation (would be branching-behind-a-type); `ApiError` unions (boring/correct); `Database` vs `DatabaseDetail` merge (would invite empty-password misreads); blob decode helpers (intentionally different shapes).
- **S14 near-misses: SKIP** — diff positions array (line-count); `changed` pre-scan (defensive); untyped status strings (public contract). diff engine itself: correct, well-tested, boring.
- **S9 near-misses: SKIP** — dual int/string error codes (distinct consumers); `Envelope.Success` derivability; unreachable `StringCode` default; write-failure asymmetry.
- **S1 near-misses: SKIP** — none beyond AC (all covered by P/AC).
- **S4 near-misses: SKIP** — generic ask-or-flag decision abstraction over 8 resolvers (parameters vary in kind; a shared model needs ~6 nullable knobs — itself an invalid-combination surface); double `Databases()` fetch; `os.IsNotExist(fmt.Errorf(...))` dead disjunct at `launch.go:138` (real but bug-fix-sized nit — recorded as a noted minor item, correct fix is `errors.Is(composeErr, fs.ErrNotExist)`); `LaunchDataView` mirror (deliberate seam).
- **S6 near-misses: SKIP** — duplicated profile-table loops; `DefaultDataView{Set,Default}`; whoami double-guard; `firstNonEmpty`/token-error duplication.
- **S5 near-misses: SKIP** — `DeployDataView` union (three-way switch small and clear); unclassified 404 on post-follow fetch.
- **S17 near-misses: SKIP** — `Event` as struct-with-optional-fields (Go idiom, single consumer); terminal-status switch→map; retry counter.
- **S18 near-misses: SKIP** — none beyond AA.
- **S19 near-misses: SKIP** — coolify-api.sh (generic, no duplicated API knowledge); `coolify.env` gitignore verified.

## 4. Cross-cutting patterns

1. **0/""-sentinel conflation for "unset" (the dominant theme):** team id 0 (E, F; also S2's out-of-boundary note), workers -1 (Z), port 0 (T), env "" (R, S), repo config path (J, AB), blob "" (L). Recommendation E's `*int` is the exemplar; every sentinel here has a live collision with a real value.
2. **Rule owned in N places, each re-derived:** env-default (R/S), global-flag list (AC), variant matrix (AA), compose filename (X), sidecar trio (Y), App/Command routing (P), doc contract (AF).
3. **Defensive checks of producer-impossible states:** HasSsr (O — delete), PlacementOnly nil-Client (N — unrepresentable after split), FixView zero value (U). After O/N/U these checks become unconstructible instead of guarded.
4. **Double fetch / re-scan of the same endpoint or file:** env list per key (K), repo config twice per launch (J), `VisibleLogs()` twice per poll (L), `FindRepoConfig` duplicated (AB).
5. **Dead code born of duplicated structure:** `adoptView` branch (D), `App.Version`/`Commands()` (P), `--json` default-branch guard (AC), `isHelp` parent branch (P), `Result.DryRun` (Q), `os.IsNotExist` disjunct (noted under S4 skips).
6. **Test-blind spots where the hazard is latent:** --json deploy stream (B), dry-run would-write branches (Q), timeout race (C), SSR surface (I), text-mode drop (AD), fix paths (U). Several accepted findings are primarily "the suite cannot see this failure."

## 5. Duplicates & superseded findings

- **Team-0 sentinel:** S12#1 (E) and S6#1 (F) share one root cause. Authoritative ownership: E = config override representation (`Overrides.TeamID *int`); F = the logout consumer branch + view (independently landable via its minimal variant). S2's out-of-boundary note is the same theme — recorded here, not a separate finding.
- **Env default:** S2#2 (R, session-level rule) and S14#2 (S, Options channel) are complementary scopes of one cleanup; `pkg/commands/generate.go:123-127` appears in both. Suggest one combined change-set.
- **SSR:** S10#2 (H, deterministic selection) and S18#1 (I, fixtures) — H is a prerequisite for I's golden regen.
- **Superseded:** S11 worker's initial "SSR as discriminated union" idea → superseded by O's derived-method form (smaller, golden-stable). S15 worker's alternative "change Emit param to TextView" variant of AD → rejected (ripples into commands signatures; the diagnostic variant is smallest).
- **Dropped:** none fully rejected as false; S13#2 (AE) and S9#1 (AD) kept at lowest priority with materiality caveats recorded.

## 6. Final priorities & dependencies

Ranked by concrete impact, confidence, effort, blast radius, prerequisites:

1. **A** (S10#1 PHP floor) — silent wrong-image bug; ~6 lines; no prereqs. **Best first slice.**
2. **B** (S5#1 JSON leak) — documented-contract violation; 4 lines.
3. **C** (S17#2 timeout race) — wrong terminal output, flaky test; ~15 lines.
4. **D** (S4#2 dead adopt branch) — code lies about behavior; trivial.
5. **E+F** (team-0 sentinel, config + logout) — wrong-token-deletion bug class; do together or F-minimal first.
6. **G** (S8#1 stranded input) — silent secret loss; ~12 lines.
7. **H** (S10#2 SSR determinism) — prerequisite for I.
8. **I** (S18#1 fixtures) — restores claimed coverage; depends on H (and benefits from O landing first to avoid a second regen; O's method keeps goldens stable either way).
9. **J** (S4#1 -c divergence) — wrong-file writes; small.
10. **K** (S13#1 SetEnvVars batch) — N+1 network + APP_KEY race window; medium.
11. **L** (S17#1 blob→[]string) — O(n²)→O(delta); small.
12. **M** (S6#2 commit ordering) — trivial.
13. **N** (S2#1 OpenPlacement) — removes a representable invalid state; small.
14. **O** (S11#1 HasSsr collapse) — highest-value domain simplification; mechanical; golden-stable.
15. **P** (S1#1 App-as-root-Command) — biggest structural cleanup; medium; fix help-format regressions deliberately.
16. **Q** (S14#1 decide-then-apply) — structural dry-run guarantee.
17. **R+S** (env default single ownership) — one change-set.
18. **T** (S3#1 provided-flags) — accurate validation errors.
19. **U** (S3#2 FixView) — JSON shape change, do pre-GUI.
20. **V** (S5#2 timeout message) — trivial.
21. **W** (S15#1 jsrun) — small.
22. **X** (S16-cc compose filename) — trivial.
23. **Y** (S15#2 sidecar precompute) — resolve the replicas asymmetry first (intent vs drift).
24. **Z** (S11#2 workers) — library-contract change; schedule consciously.
25. **AA** (S18#2 variants.tsv) — cheap insurance.
26. **AB** (S12#2 repo-load dedup) — small.
27. **AC** (S1#2 flag table) — borderline; fine to defer.
28. **AD** (S9#1 diagnostic) — preventive; trivial.
29. **AE** (S13#2 postForUUID) — trivial; weakest accepted.
30. **AF+AG** (S19 doc repairs) — trivial; AG can ride any PR.

Dependency graph: H→I; E↔F (companion, F-minimal independent); O→I (soft, ordering only); R↔S (same review); P independent; all Tier-1 items have no prereqs.

## 7. Audit log

- Batch 1 (S1, S2, S8, S9, S10): 5 workers, complete.
- Batch 2 (S11, S14, S15, S17, S19): 5 workers; S14 returned empty → re-dispatched in Batch 3.
- Batch 3 (S14 retry, S3, S5, S12, S13): 5 workers, complete.
- Batch 4 (S4, S6, S7, S16, S18): 5 workers; S4 returned empty → re-dispatched solo, complete.
- Coordinator verification: 15 spot-checks against source (cli.go routing/sentinels, session.go PlacementOnly/Env, input.go buffer, output.go drop, detect.go parsers/ssr scan, plan.go/check.go/generate.go HasSsr+Workers, resolve.go team sentinel + repo-load dup, resources.go/launch.go env listing, deploy.go blob/select, launch.go recordEnvironmentUUID/adopt guard/registry path, FixView/fs.Visit, Dockerfile.tmpl switches, fixture package.json + golden diff, AGENTS.md/docs drift, dead-code greps for App.Version/Commands()/Result.DryRun). All load-bearing claims confirmed; no finding rejected as false; S13#2 confirmed as 4 identical uuid-shape sites (of 7 res-struct sites).
- Audit-the-audit passes: coverage (no missing subsystems; go.mod trivial — 3 deps, nothing to simplify), duplication/ownership (themes consolidated, every accepted finding has exactly one authoritative row), materiality (2 borderline items demoted to Tier 4 with caveats: AC, AE; 1 preventive item labeled: AD), schema completeness (all accepted findings carry verdict/evidence/complexity/proposal/scope/risks/validation/confidence), priority consistency (dependency graph checked: H→I, E↔F, O→I, R↔S; no cycles; Tier-1 prereq-free).
- Repo state: verified unchanged (`git status` clean before and after; no files written inside the repository — this report lives in `/tmp/opencode/berth-audit/`).

## 8. Completion check

- [x] Every identifiable subsystem reviewed (19/19)
- [x] Every subsystem has a recommendation or explicit skip (17 rows with accepted findings, S7 + S16 skips validated with reasoning)
- [x] Every finding has complete evidence, scope, risk, and validation fields
- [x] Duplicates removed / superseded findings recorded (§5); weak abstractions removed (§3 skips)
- [x] Priorities and dependencies internally consistent (§6)
- [x] Repository unchanged
