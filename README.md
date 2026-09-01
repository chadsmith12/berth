# Berth

**Dockerfile + Coolify resource generator for Laravel.**

Berth removes the manual work of containerizing and deploying Laravel apps. Point it at a project and it inspects the codebase, plans what the deployment needs, and generates deterministic, deployment-ready files.

It detects PHP and Node versions, the package manager, SSR and Wayfinder usage, queue workers and scheduling, then renders Dockerfiles, entrypoints, and Coolify-compatible configuration via embedded templates. It always emits files that Coolify can consume, and can optionally call the Coolify API to create or update resources.

The project is library-first: all core logic lives in `pkg/` with no dependency on CLI or GUI. A thin CLI consumes the library today; a GUI desktop app will reuse the same `pkg/` later.

> Status: in development. The CLI and library are the current focus.
