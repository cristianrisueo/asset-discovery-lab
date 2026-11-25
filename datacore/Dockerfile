# datacore image (spec §15.11): a static Go binary on distroless, built from the
# repository root context because all Go code is one module (spec §17.1).
FROM golang:1.26.11-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY gen/ gen/
COPY datacore/ datacore/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/datacore ./datacore
# The datacore-data volume is mounted at /data. Creating it here, owned by the
# distroless nonroot user, makes the named volume writable (spec §15.7).
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/datacore /usr/local/bin/datacore
COPY --from=build --chown=65532:65532 /out/data /data
EXPOSE 50051 8090
ENTRYPOINT ["/usr/local/bin/datacore"]
CMD ["serve"]
