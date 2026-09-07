# real test apps

Gitignored. `scaffold.sh` builds real Laravel apps here for docker/deploy
end-to-end testing. Committed artifacts are limited to this README and the
script itself — the apps (with vendor/, node_modules/, lockfiles) never enter
the repository.

```bash
test-apps/real/scaffold.sh <name> [--horizon] [--ssr] [--wayfinder] [--npm]
```

- Needs `php` + `composer` + network. The JS toolchain defaults to bun
  (`bun.lock`); `--npm` switches to npm — but `package-lock.json` only exists
  on machines that have npm, and the Dockerfile's npm path runs `npm ci`,
  which requires it.
- The script runs `git init` + an initial commit, since Coolify deploys from
  git; push to a host the Coolify server can reach before `launch`.

## Manual flow

```bash
test-apps/real/scaffold.sh app1 --bun --ssr --horizon
berth-cli generate --path test-apps/real/app1 --scheduler --port 8080
docker build --target runtime -f test-apps/real/app1/docker/app/Dockerfile test-apps/real/app1
```

A green `docker build` is the same build Coolify performs — the strongest
pre-Coolify signal available today. Full Coolify e2e (launch, deploy, status,
logs) waits for those commands to exist.
