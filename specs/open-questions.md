# Open questions

Doubts about the spec, or tests that look wrong. The agent writes them down and keeps working (spec §3 G2, sprints plan §2). The owner decides after the build.

## Sprint 0

### Q-001 · Interpretations in the acceptance suite (A05, B08, D04, E04)
Some §16.4 rows leave room for interpretation. Decision D-012 records how each was implemented. The owner may want to confirm:

- **A05** "two more records appear": implemented as *at least* two. An at-least-once adapter may legitimately publish a third copy on a rebalance.
- **B08** "since the stack started": implemented as the Flink job's start time, or the moment the test set the generator to its default rate.
- **D04**: the row has no "Given", but the test pauses the generator. Without that, moves into or out of Madrid between the two page requests could cause a false overlap.
- **E04** "a savepoint was written": checked by listing `/flink-data/savepoints` through `docker compose exec` before `make down`. After `make up`, the restored savepoint must be a new one.

No test was weakened. These notes only make the readings visible.

### Q-002 · Spec §2.5 lists `pkg/kadm` under franz-go's version
`pkg/kadm` is a separate Go module; its latest release is v1.20.0. Decision D-002 pins it. The spec table could list kadm separately.

## Sprint E

### Q-003 · DoD item 8: CI workflows not yet run on GitHub
§18 item 8 asks that `ci.yml` and `e2e.yml` "pass when run on GitHub". The build never pushes: pushing is an outward action, and the build script only commits and tags. Local `main` is ahead of `origin/main`.

What was checked locally instead (sprint E build report, D-046):
- **Workflow syntax:** actionlint v1.7.12 reports 0 errors on both workflows.
- **Action tags:** every pinned action tag exists upstream.
- **`ci.yml`:** every command it runs passes locally, through `make test lint` and the `go test -race` of the changed packages.
- **`e2e.yml`:** its steps (`make env`, `make build`, `make up`, `make acceptance`) pass locally in that order.

Owner action:
1. Push `main`, check both workflow runs, and open `e2e` with **Run workflow** if needed.
2. `e2e` needs a runner with at least 16 GB of RAM. GitHub's standard `ubuntu-24.04` runner has 16 GB for **public** repositories only. On a private repository, use a larger runner or the spec's fallback (`make up-core` with gates A and B).
