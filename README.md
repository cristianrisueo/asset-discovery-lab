# asset-discovery-lab

A small, local replica of Lansweeper's new data pipeline: datacore (Go), Kafka,
adapter (Go), Dragonfly, a Flink 2.1 job (Java 17), Doris 4.1, Iceberg on RustFS,
explorer (Rust and Axum, over Arrow Flight SQL) and a React web.

The system is specified in [`specs/system-spec.md`](specs/system-spec.md) and is
being built sprint by sprint following [`specs/sprints-plan.md`](specs/sprints-plan.md).
Progress is tracked in [`specs/build-progress.md`](specs/build-progress.md).

The complete README (quick start, walkthrough, troubleshooting) is written in the
final sprint. Until then, run `make help` to list the available targets.

Licensed under the [MIT License](LICENSE).
