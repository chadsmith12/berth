# test-apps

Fixtures for end-to-end testing of `berth generate`. Each directory is a
**buildable stub app**: real composer/npm/bun lock files (generated once and
committed), a stub `artisan`, and build scripts that satisfy the structural
contracts (`public/build`, `bootstrap/ssr/app.js`). They are not real Laravel
apps — but their images *do* build, which is the point.

## Variants

| Variant              | Proves                                                        |
| -------------------- | ------------------------------------------------------------- |
| `php-only`           | No `package.json`: Dockerfile builds without node/npm; **and** composer.lock raising PHP above the json floor (its lock contains symfony packages requiring >= 8.4 while composer.json says ^8.2) |
| `npm-vite`           | npm + node toolchain, real `package-lock.json`, `worker-N` services |
| `bun-ssr-wayfinder`  | bun toolchain, real `bun.lock`, SSR bundle contract, Wayfinder stage, SSR service |
| `horizon`            | Single `horizon` service, redis queue/cache, `redis` php ext  |
| `horizon-ssr`        | Horizon + SSR combined: horizon + ssr services, redis         |

## Smoke builds — the template validity guarantee

Goldens prove the files look right; only docker proves they *work*. Before
pushing any template change:

```bash
test-apps/smoke-build.sh
```

This copies each variant to a temp dir, runs the real `generate` command, and
`docker build --target runtime` the result — the same build Coolify runs.
Requires docker; skips cleanly without it. This harness is what catches the
bugs goldens cannot see (the `--from` variable failure and the
composer-lock/PHP mismatch were both of this class).

## Golden files

`<variant>/golden/` holds the expected output of `berth generate` with the
decisions encoded in `pkg/commands/e2e_test.go`. The runner copies each
variant to a temp dir, runs the real command in-process, and byte-compares.

```bash
go test ./pkg/commands -run TestGenerateE2E          # verify
go test ./pkg/commands -run TestGenerateE2E -update  # regenerate goldens after template changes
```

Golden diffs are reviewable — a template change shows exactly what it does to
every variant.

## Real apps (`real/`)

`real/` is gitignored. `scaffold.sh` creates real Laravel apps for
docker/deploy testing (needs php + composer + network):

```bash
test-apps/real/scaffold.sh my-app --bun --ssr --horizon
```

Then:

```bash
berth-cli generate --path test-apps/real/my-app --scheduler --port 8080
docker build --target runtime -f test-apps/real/my-app/docker/app/Dockerfile test-apps/real/my-app
```

`docker build` needs nothing local except docker — node/bun/composer run
inside the image. `docker build` is the same build Coolify runs, so a green
build is the best pre-Coolify signal. Full Coolify e2e (launch, deploy,
status, logs) waits on those commands.
