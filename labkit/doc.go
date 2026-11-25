// Package labkit holds the clients and the reconciliation logic shared by adlctl
// and the acceptance suite (spec §16.1, §16.5, §17.1).
//
// Every client is built from a Config (see ConfigFromEnv) and talks to the stack
// only through its public interfaces: Kafka, Dragonfly, the datacore admin API,
// Doris over the MySQL protocol, S3, the Iceberg REST catalog, the Flink REST
// API, plain HTTP and the docker compose CLI. Constructors never connect: a
// client that cannot reach its target fails on its first call, with an error
// that names the target and its address.
package labkit
