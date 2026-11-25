# explorer image (spec §13, §15.11): a release build of the Rust crate on
# debian:bookworm-slim. The toolchain stage is also the builder of
# `make test` and `make lint` (adl/explorer-toolchain).

FROM rust:1.100.0-bookworm AS toolchain
RUN rustup component add rustfmt clippy

FROM toolchain AS build
WORKDIR /src
COPY Cargo.toml Cargo.lock rust-toolchain.toml ./
COPY src ./src
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/src/target \
    cargo build --release --locked \
 && mkdir -p /out && cp target/release/explorer /out/explorer

FROM debian:bookworm-20261123-slim
RUN useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin explorer
COPY --from=build /out/explorer /usr/local/bin/explorer
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/explorer"]
CMD ["serve"]
