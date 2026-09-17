# berth — Design

berth deploys Laravel applications to Coolify. It is a Go library with a CLI
frontend, and a desktop GUI frontend later. Both frontends are thin; all
behaviour lives in the library.

---

## 1. What berth does

**Generates** the files a Laravel project needs to run as containers, and
**launches** the Coolify infrastructure that runs them. Once an application
exists, berth deploys it, reports its status, and reads its logs.

berth is not a Coolify replacement, not a general API client, and not a secrets
manager. It will not convert an existing application to its layout — that would
change how a working application builds.

---

## 2. Principles

### Generative, not declarative

berth generates files. It does not hold a desired state for Coolify and
reconcile against it.

`docker-compose.<env>.yml` is the source of truth for how an application runs.
It is committed, readable, hand-editable, and it is what Coolify deploys.
Settings that live in Coolify — domain, branch, environment variables, proxy
configuration — belong to Coolify. berth sets them once at creation and never
again.

This matters because Coolify has a UI and people use it. A tool that declared
the domain would have to either overwrite someone's UI change or ignore its own
configuration. Not storing it removes the problem rather than managing it.

### Detect facts, ask for decisions

Anything readable from the project is read every time: package manager from the
lockfile, PHP version from `composer.json` **raised to what `composer.lock`
requires** (a lock resolved on a newer PHP can contain packages whose own
requirements exceed the json floor — the lock is the truth for reproducible
installs), SSR from the build script, Node version from `package.json`.

Anything that is a judgement — worker count, whether to run a scheduler, which
port, which server — is asked for, or supplied as a flag.

berth never silently defaults a value it cannot infer. There is no `--yes`.

### One thing persists: the application UUID

`deploy`, `status` and `logs` need to know which Coolify application they act
on. That is the only state berth keeps.

### Identity is the UUID

Coolify has a stable resource UUID and a mutable display name. berth resolves
applications by UUID and never searches for one by name. There is no name-based
fallback: retargeting a resource that happens to share a name is worse than
stopping.

Projects and environments are different. Their names are **supplied by you**,
so finding them by that name is not a guess. berth finds them or creates them,
and refuses if a name matches more than one.

The rule is that berth never infers which resource you meant. A name you gave
it is not an inference.

### Creation is explicit

`launch` creates infrastructure. `deploy` does not. Running `deploy` against an
environment berth has never launched is an error, not an invitation to set one
up. Creating infrastructure is something you ask for.

### Deploy is read-only

`deploy` writes nothing to the repository and changes nothing in Coolify's
configuration. It needs a UUID and a token. That is what makes it safe in a
pipeline.

---

## 3. Architecture

```
cmd/berth-cli          CLI: flags in, library out, one render call
cmd/berth-gui          GUI: same library, different frontend
  ↓
pkg/commands           orchestration + view types (the public output contract)
  ↓
pkg/cli                command tree, global flags, terminal detection
pkg/input              interactive prompts (the CLI's decision supplier)
pkg/output             the single render call: text views, JSON envelope, exit codes
  ↓
pkg/detect             read a project, report what is there
pkg/generate           render files from detected facts and decisions
pkg/coolify            API client
pkg/config             read and write the environment registry, resolve placement
  ↓
pkg/plan               shared vocabulary; imports nothing
```

### Rules

- `pkg/plan` imports no other berth package.
- `pkg/output` imports no domain package. View types live in `pkg/commands`.
- Domain packages return plain errors. Only `pkg/commands` attaches exit codes.
- No package prints. A command does its work, returns one value, and hands it
  to a single render call.

### Sessions

Commands do not resolve configuration themselves. `commands.Open` builds a
`Session` once per invocation — placement, token, client, environment
registry — and the command works only from it. It is the single resolution
point: global flags and per-run overrides bound, precedence applied, the
team drift check run when the command acts. Like boto3's Session it is a
plain struct, not global state; a GUI calls the same `Open` and supplies its
own decisions. Errors leaving `Open` already carry their exit code:
resolution and token problems are usage errors, API errors are auth errors.

### Frontends

A CLI forces every operation to be data in, data out. That constraint is what
lets a GUI be a second `cmd/` rather than a rewrite. The GUI will use a native
Go toolkit rather than an embedded browser.

Interactive prompts are a frontend concern. The library returns what it needs
and the frontend supplies it — the CLI by prompting, the GUI by rendering a
form. No library code reads from stdin.

### Streaming

Long operations return a channel:

```go
func Deploy(ctx context.Context, ...) (<-chan Event, error)
```

A channel of events feeds a CLI's stdout loop and a GUI's progress pane
equally. Coolify has no log stream, so the producer polls and diffs; that is
invisible behind this signature.

---

## 4. Configuration

### Repository: `.config/berth.json`

Committed. Environments are shared by the team, and a resource UUID is not a
secret — anyone who clones the repository can deploy without linking first.

Read from `.config/berth.json`, falling back to `berth.json` at the repository
root. Both present at once is an error, since silently preferring one means
somebody edits the wrong file.

```json
{
  "$schema": "https://berth.sh/schema/v1.json",
  "environments": [
    { "name": "production", "uuid": "j9e2lm7xssikhtpvr3e9x1jp" },
    { "name": "staging", "uuid": "kw8oc4k0ggs4wcw0wkwcgsw8" }
  ]
}
```

`name` is the Coolify environment name and the value you pass to `--env`. They
are the same string deliberately; a second layer of naming would buy nothing.

No application name is stored. When one is needed for display, it comes from
Coolify, so it cannot be stale after a rename.

JSON because `encoding/json` is stdlib and it matches the API. The cost is no
comments; `$schema` returns that as editor autocomplete.

### Placement, and why it is optional

berth needs to know which instance, team and project to act against:

```json
{
  "coolify": {
    "url": "https://coolify.example.com",
    "team": { "id": 3, "name": "Client Work" },
    "project": "pmc"
  }
}
```

This block is optional, because the file is committed and its contents are
disclosed if the repository goes public: internal hostname, project names, team
structure. Identity resolution, highest precedence first:

```
Identity — the instance and team pair:

1. --profile / BERTH_PROFILE     a named profile
2. .config/berth.json            the coolify block
3. the default profile           set by `berth auth login`

Then overrides for a single run, above any identity:

--url / --team / --project / --uuid
BERTH_URL / BERTH_TEAM / BERTH_PROJECT / BERTH_UUID
```

Most people deploy to one instance, so the default profile covers it and a
repository commits nothing about where it runs. When a value cannot be
resolved, the error names every place it could be set.

A bare resource UUID discloses nothing without an instance URL and a token for
the right team, so UUIDs are safe to commit.

`team` is always an object. `"team": "3"` versus `"team": "Client Work"` is
ambiguous — a team named `"2024"` would parse as an id. The id is
authoritative; the name is for readable diffs and drift detection.

### User

```
~/.config/berth/config.json        0644    profiles and which is default
~/.config/berth/credentials.json   0600    tokens
```

Separate files so preferences are readable and tokens are not.

#### Profiles

A profile is a named identity: an instance and team pair under a name you
chose. Tokens stay keyed by instance and team; the profile is a handle for
them. Switching a default never touches a token, and one token can serve
several profiles.

```json
{
  "default": "software-celebrate",
  "profiles": [
    { "name": "software-celebrate", "url": "http://10.190.122.34:8000",
      "team": { "id": 9, "name": "Software Celebrate" } }
  ]
}
```

`berth auth login` suggests a name derived from the team name and prompts;
`--as` names the profile directly. Plain login never changes the default:
`--default` does, and the first profile you create becomes it. A name that
already refers to a different instance or team is refused — pass `--as` to
choose another; berth never silently disambiguates.

`berth auth default [name]` lists the profiles and marks the default, or
switches the default without re-entering a token.

---

## 5. Authentication

Coolify tokens are scoped to a single team, and cross-team access returns
**404, not 403**. A mis-scoped token is therefore indistinguishable from a
wrong identifier unless berth says so. Every 404 message names the team berth
is acting as.

### Tokens identify themselves

`GET /teams/current` returns the authenticated team. You paste a token and
berth files it under the right team; you never label or select tokens. The same
call validates it: if it succeeds, the token is live and has at least `read`.

### Storage

```json
{
  "instances": [
    {
      "url": "https://coolify.example.com",
      "teams": [
        { "id": 1, "name": "Internal", "token": "12|xxx" },
        { "id": 3, "name": "Client Work", "token": "45|yyy" }
      ]
    }
  ]
}
```

Keyed by instance and team id. Arrays rather than objects keyed by id, so ids
stay integers. Names are display only; they get renamed, ids do not.

berth compares `GET /teams/current` against the configured team on every run
and refuses on mismatch.

### Permissions

| Use            | Abilities                                   |
| -------------- | ------------------------------------------- |
| CI deploy      | `deploy`                                    |
| Monitoring     | `read`                                      |
| `berth launch` | `read`, `write`, `deploy`, `read:sensitive` |

`read:sensitive` **redacts rather than rejects**: without it, database
credentials return as nulls inside a 200 response. berth detects empty
credentials and fails, rather than writing empty passwords into Coolify.

A 403 lists the missing abilities; berth surfaces that list verbatim.

### Entering a token

Tokens are `id|secret` and both halves are required. berth validates the format
before making a request, since pasting only the secret is common and otherwise
produces an opaque failure.

Tokens arrive by prompt or `--token-stdin`. Never a `--token` flag: it leaks
into shell history and process lists.

`BERTH_TOKEN` overrides stored credentials and skips interactive auth, so the
same commands work at a desk and in a pipeline.

---

## 6. Commands

```
berth generate     write the deployment files
berth launch       create the Coolify infrastructure for an environment
berth deploy       trigger a deployment and follow it
berth status       what is configured, and the last deployment
berth logs         container logs
berth auth         login | list | default | whoami | logout
```

### generate

Stateless. Reads the project, asks for what it cannot infer, writes files. It
reads no configuration and contacts no network.

```bash
berth generate [--path .] [--workers -1] [--scheduler] [--port N] [--fix] [--dry-run] [--force]
```

Detected: package manager, PHP and Node versions, SSR, Wayfinder, Horizon,
and the **base directory** — the app's path relative to the git repository
root (empty when the app sits at the repo root). `--path` may be either the
app directory or the repository root: if the root has no `composer.json` of
its own, `generate` descends into the monorepo's single Laravel app (several
apps is an error naming each, pointing you at `--path <subdir>`). The base
directory is surfaced as `base dir` in the output and drives the
`base_directory` value sent to Coolify at launch time.
Asked or flagged: worker count, scheduler, application port. Horizon changes
the topology itself: `laravel/horizon` replaces the `worker-N` services with
a single `horizon` service and switches queue and cache to redis, so the
worker count is not asked. `APP_ENV` follows `--env`; `APP_DEBUG` is false
everywhere.

`--env` names the compose file, so each environment can have its own service
topology.

Existing files are never overwritten without `--force`. An unchanged file
needs no force. A file that differs is shown as a diff and refused; with
`--force` it is overwritten and the diff is still shown. `--dry-run` shows
the same picture, diffs included, without touching disk.

`generate` also refuses to write files that would produce a deployment which
appears to work but does not — see section 7.

### launch

Creates the infrastructure for one environment and records the application
UUID.

Coolify nests **project → environment → application**, and any level may be
missing. `launch` resolves each in turn, finding what you named or creating it:

```
project        --project pmc       find by name, else create
environment    --env production    find by name, else create
application                        create
```

There is no separate mode for "nothing exists yet" versus "adding an
environment to an existing project." Starting from scratch creates all three.
Adding staging to a live project finds the first and creates the other two.
Same command, same flags.

Each step is idempotent, so re-running after a partial failure resumes rather
than duplicating. A name matching more than one project or environment is an
error, not a coin toss.

```bash
berth launch --env production \
  --project pmc \
  --server <uuid> \
  --branch main \
  --domain http://pis.example.com:8080 \
  --database <uuid> \
  --key <uuid>
```

`launch` locates the app the same way `generate` writes it: a `--path`
pointing at the repository root is descended into the subdirectory holding the
generated `docker-compose.<env>.yml` (the monorepo app), so
`berth generate --path .` followed by `berth launch --path .` works unchanged.
It derives the **base directory** (that app path relative to the git
repository root; empty = repo root), which maps to Coolify's
`base_directory` (a `/`-prefixed path). Coolify combines that with the compose
file location when locating the stack — this is what makes a monorepo app
deploy correctly.

The domain does not need to carry the container port. `launch` reads the port
the `app` service exposes from `docker-compose.<env>.yml` and appends it; a
domain given with a port is cross-checked against that port and refused on
mismatch. Projects are always named or selected — even when the team has
exactly one, launch asks rather than assumes intent.

#### Anything not supplied is asked for

```
? Project                 pmc  ·  ayoka-internal  ·  Create new…
? Environment             production  ·  Create new…
? Server                  (skipped — the team has one)
? Git branch              main
? Domain                  http://pis.example.com:8080
? Database                pmc-postgres  ·  None
? Deploy key              gitlab-ayoka  ·  Create new…
```

Lists come from the API, so you pick rather than paste UUIDs. Servers are only
asked about when the team has more than one.

Non-interactively, every missing value is a usage error naming its flag. berth
does not guess: there is no `--yes`, because worker counts, databases and
domains have no safe default.

#### Deploy keys

Select an existing key, or create one. Creating generates a keypair, stores
the private half in Coolify, and keeps going — application creation never
touches the git host, so nothing is blocked. The public half is printed at
the end, in the checklist, with paste instructions; the first deploy cannot
clone until it is added, and deploy's failure will say so.

#### What else launch sets

- injects the selected database's connection details as environment variables
  — the host is the resource uuid, which is how Coolify names the container
  on the shared network; the database name is a separate decision
  (`--db-name`), asked or flagged, never defaulted: if it differs from the
  resource's, berth injects it with a warning that it must be created first,
  and never mutates the resource
- generates `APP_KEY` once and sets it in Coolify — regenerating it would
  make every encrypted column and session unreadable
- sets `connect_to_docker_network`, without which a compose stack cannot
  reach any database resource
- sets `docker_compose_location` to the file `generate` wrote for this
  environment

#### Before you deploy

`launch` ends with a checklist rather than a stopping point:

- the deploy key to paste into the git host, when one was just created
- any database notices (a chosen name that does not exist yet)
- the git state of the deploy branch: uncommitted files and unpushed commits
  are counted, because Coolify deploys from git — a compose file that exists
  only locally does not exist for Coolify

Then `berth deploy`.

#### Adopting an application that already exists

```bash
berth launch --env production --uuid j9e2lm7xssikhtpvr3e9x1jp
```

berth verifies it resolves and records it. It will then deploy that
application, show its status and read its logs, without touching how it builds.

`launch` refuses if the environment already has a UUID, unless `--force`.

### deploy

```
berth deploy                      trigger + follow to completion
berth deploy --detach             trigger and return
berth deploy --deployment <uuid>  follow an existing deployment without triggering
berth deploy --timeout 20m        give up after this long (the default)
```

`deploy` requires a linked environment. If `--env` names one berth has no UUID
for, that is an error directing you to `launch`. It will not create
infrastructure.

It needs a UUID and a token, and takes both from flags or the environment when
there is no configuration file:

```bash
berth deploy --uuid j9e2lm7xssikhtpvr3e9x1jp
BERTH_TOKEN=... BERTH_UUID=... berth deploy
```

A pipeline can therefore deploy from a checkout with no `.config/berth.json`,
since the build happens on the Coolify server and berth only issues the trigger
and follows the result.

**Following streams the visible build output.** The deployment record's
`logs` field is a JSON-encoded array of entries; entries with
`hidden: true` are Coolify's internal commands — helper containers, docker
plumbing — not build output. berth filters those out and streams only the
regular build lines; on failure it prints the last 20 of them plus the
Coolify UI deployment page for the full detail (debug entries included).
Older instances that omit the field fall back to status-only following.
`--ci` still waits for a real build-completion signal — the entries carry
none, and watching log text for markers is a guess.

`--json` emits one final envelope (uuid, status, duration, commit, logs URL,
log tail), never an event stream.

### Global flags

```
--env <name>     which environment          default: production
--profile <name> which instance/team identity
--json           machine-readable output
-c <path>        alternate config file
```

The `--env` default is the one sanctioned default: it names a compose file and a
registry entry, never a value the project states.

### What writes where

| Command          | Repository                                       | Coolify                                   |
| ---------------- | ------------------------------------------------ | ----------------------------------------- |
| `generate`       | Dockerfile, compose, entrypoint, `.dockerignore` | —                                         |
| `launch`         | the `uuid`                                       | creates project, environment, application |
| `deploy`         | —                                                | triggers a deployment                     |
| `status`, `logs` | —                                                | —                                         |

---

## 7. Generated files

```
docker/app/Dockerfile              shared by every environment
docker/app/entrypoint.sh           shared
.dockerignore                      shared
docker-compose.<env>.yml           one per environment
```

`.dockerignore` sits at the repository root because Docker reads it from the
build context root. The compose files sit there too, because `context: .`
resolves relative to the build context.

### One image, several roles

Within an environment, `app`, `ssr`, `worker-N` and `scheduler` build the same
image and differ only by the command they run. They cannot run different code.

The Dockerfile is shared across environments as well, so staging is a faithful
rehearsal of production. Only the set of services differs.

### Constraints

These are easy to undo by accident, and each produces a failure that is quiet
rather than loud.

| Constraint                                                    | Reason                                                                                                                           |
| ------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Workers are discrete services (`worker-1`, `worker-2`)        | Coolify assigns each service an explicit `container_name`, and Compose cannot scale a service with a fixed name                  |
| Horizon runs as a single service                              | Horizon supervises its own queue workers; several instances would duplicate the supervision                                       |
| Exactly one scheduler                                         | Two means every scheduled job fires twice                                                                                        |
| Migrations run in `app` only, and `app` is a single container | Without Redis there is no shared lock; `migrate --isolated` needs `cache_locks`, which does not exist before the first migration |
| `worker` and `scheduler` disable the inherited healthcheck    | The base image probes a web server these containers do not run, so they would sit permanently unhealthy                          |
| Non-web services also set `exclude_from_hc: true`             | Coolify aggregates every non-excluded container's health into the app's status; a worker/scheduler with no check reports "unknown" and makes the whole stack show "no healthcheck" |
| `expose`, never `ports`                                       | Publishing a port bypasses the proxy                                                                                             |
| `node_modules` ships when SSR is on                           | The SSR container is a long-running Node process, and Vite externalizes dependencies in SSR builds                               |
| The assets stage builds on the PHP image                      | Laravel Vite plugins shell out to `php artisan` during the build                                                                 |
| Node is copied through a named stage, never `--from=node:...` | BuildKit rejects variable expansion in `COPY --from` — the failure that surfaced in the first real deployment                    |
| Source is copied before built artifacts                       | Reversed, `COPY . .` clobbers the built `vendor/` and assets                                                                     |

### Project prerequisites

`generate` refuses to write files when these are unmet, because each otherwise
produces a deployment that appears to work.

| Check                                                             | Consequence if unmet                                                                  |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `config/inertia.php` reads `INERTIA_SSR_URL` from the environment | SSR silently falls back to client rendering; the SSR container runs and is never used |
| The detected SSR script builds the SSR bundle                     | The SSR container crash-loops                                                         |

The domain carrying the container port is handled at `launch`: the port comes
from the compose file, not from you — a domain without a port gets one
appended, a domain with one is cross-checked. The **scheme** is preserved and
drives TLS: `https://` tells Coolify to provision a certificate, `http://` does
not. Only `http` and `https` are accepted; a scheme-less host defaults to
`http`, any other scheme is refused. `generate` never sees a domain.

---

## 8. Coolify integration

Base path `/api/v1`.

| Purpose                   | Endpoint                                    |
| ------------------------- | ------------------------------------------- |
| Identify the token's team | `GET /teams/current`                        |
| Projects                  | `GET /projects`, `POST /projects`           |
| Environments              | within a project                            |
| Servers                   | `GET /servers`                              |
| Deploy keys               | `GET /security/keys`, `POST /security/keys` |
| Databases                 | `GET /databases`, `GET /databases/{uuid}`   |
| Create an application     | `POST /applications/private-deploy-key`     |
| Application detail        | `GET /applications/{uuid}`                  |
| Environment variables     | `POST`/`PATCH /applications/{uuid}/envs`    |
| Trigger a deployment      | `POST /deploy?uuid=`                        |
| Deployment status         | `GET /deployments/{uuid}`                   |
| Application deployments   | `GET /deployments/applications/{uuid}`      |
| Container logs            | `GET /applications/{uuid}/logs`             |

### Creation payload

Shapes pinned against a live Coolify instance (see `scripts/coolify-api.sh`);
do not trust the public docs over the probe.

`POST /applications/private-deploy-key` takes `project_uuid`,
`environment_name`, `server_uuid`, `git_repository`, `git_branch`,
`private_key_uuid`, `name`, and:

- `build_pack: dockercompose`
- `docker_compose_location`, derived from `--env`
- `connect_to_docker_network: true`
- `autogenerate_domain: false` — the default is true, and left alone Coolify
  invents a domain
- `docker_compose_domains`, so only the `app` service is reachable — and each
  entry requires a `name` alongside its `domain`

Repository URLs use Coolify's scp-style `git@host:port/path` form; this
instance rejects `ssh://`. `launch` normalizes local remotes
(`git@host:path` without a port, `ssh://git@host:port/path`) into it.

Environment variables are one-per-request: `POST
/applications/{uuid}/envs` creates, `PATCH /applications/{uuid}/envs`
updates an existing one, and `GET` lists keys without values. Tokens without
`read:sensitive` get database credentials **omitted** from database
responses — a missing password is a loud failure, never an empty value.

### Error handling

The client's job is turning status codes into messages that say what to do.

| Status | Meaning                                                      |
| ------ | ------------------------------------------------------------ |
| 400    | Malformed token                                              |
| 401    | Token rejected: expired, revoked, or truncated               |
| 403    | Missing abilities; the response lists them                   |
| 404    | Not found, **or** the token is scoped to another team        |
| 409    | Domain conflict; the response lists the resources holding it |
| 429    | Rate limit or deployment queue full; honour `Retry-After`    |

Two failures return misleading success: a disabled API, and an IP allowlist
rejection that returns `{"success": true, "message": "You are not allowed to
access the API."}`. Both are detected explicitly.

---

## 9. Conventions

### Output

Commands do all their work, return one value, and hand it to a single render
point. They never branch on output format and never print while working.

Anything a human should see is a field on that value, so `--json` carries
identical information. A value that is printed but not modelled is a bug.

Data is rendered even alongside an error — a failed `generate` still shows
which checks failed.

```json
{
  "success": false,
  "command": "generate",
  "data": {},
  "error": { "code": "checks_failed", "message": "..." }
}
```

`code` is stable and machine-readable; `message` is prose.

### Exit codes

```
0  success
1  the operation failed
2  usage error or missing required input
3  authentication: no token, wrong team, insufficient abilities
4  checks failed
```

### Interactivity

```
prompt when:  stdin is a terminal  AND  the value is missing
```

`--json` is not part of that condition. It controls output format; the terminal
controls input capability.

Prompts go to stderr, so stdout stays clean when redirected. Terminals echo
input themselves, so this is invisible in use.

A missing required value with no terminal is a usage error naming the flag.
berth never blocks on a prompt nobody can answer, and never defaults a value it
cannot infer.

### Errors

Every error names the fix. "No token" costs an afternoon. "No token for team
Client Work — create one in Keys & Tokens, then run `berth auth login`" does
not.
