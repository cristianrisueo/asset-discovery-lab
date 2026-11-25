# CLAUDE.md — asset-discovery-lab (build phase)

This file is loaded in every Claude Code session of the unattended build. It is temporary: after the build, the owner replaces it with one written from `specs/repo-exploration.md`.

## What this repository is

A local replica of Lansweeper's new data pipeline: datacore (Go), Kafka, adapter (Go), Dragonfly, a Flink 2.1 job (Java 17), Doris 4.1, Iceberg on RustFS, explorer (Rust and Axum, over Arrow Flight SQL) and a React web. Everything runs in Docker Compose on an Intel Mac with 16 GB of RAM.

## Sources of truth, in order

1. `specs/system-spec.md`: **what** to build. It wins every conflict.
2. `specs/sprints-plan.md`: **how** and **in which order**. Six sprints: 0, A, B, C, D and E.
3. `specs/build-progress.md`: where the previous session left off.

Read the first two completely before doing anything. They are long on purpose. Do not skim them.

## Non-negotiable rules

- **Nobody will answer questions.** Decide with spec §3, record the decision in `specs/decision-log.md`, and continue. Doubts go to `specs/open-questions.md`; then keep working on everything else.
- **Locked files:** `specs/system-spec.md`, `specs/sprints-plan.md`, this file, and `acceptance/` after sprint 0. Never edit them, not even through shell commands. The build script checks them with git and fails the sprint if they change.
- **Never weaken, skip or delete an acceptance test.** A test that looks wrong goes to `open-questions.md`.
- **Plan first, always.** Every session starts in plan mode and writes the sprint plan without changing anything. The script approves it and resumes the session in auto mode. Save the approved plan to `specs/sprints/sprint-<id>/sprint-plan.md` before executing it (plan §3.2).
- **One sprint per session.** Finish yours (gate green, audit subagent, reports, commit) and stop.
- **Memory budget:** 12 GB for Docker. Run `make build` before `make up`; run `make down` before heavy rebuilds; never raise the limits in spec §15.4.
- **Everything in English:** code, comments, docs and commits (Conventional Commits).
- **Pin exact versions.** Never `latest`. Record every "latest at build" pin in the decision log.
- **Flink 2.x APIs only** (spec §3 R17). Check configuration keys against the Flink 2.1 docs.

## Useful commands

| Command | Does |
|---|---|
| `make up` / `make up-core` | Full stack, or core only (without the lake) |
| `make down` / `make clean` | Stop with a savepoint / wipe all state |
| `make test`, `make lint` | Unit tests and linters |
| `make acceptance-gate GATE=X` | One acceptance gate |
| `make reconcile` | Compare Doris and the lake against datacore |
| `make logs SERVICE=x` | Logs of one service |

## Style

- Go: transport, then service, then repository or publisher, in `internal/` packages; gofmt and golangci-lint.
- Java: google-java-format through Spotless.
- Rust: rustfmt and clippy with `-D warnings`.
- Web: ESLint and Prettier.
- No directory with a single file, unless a language or tool forces the nesting.
- Structured JSON logs with a `service` field. Configuration only through environment variables.
