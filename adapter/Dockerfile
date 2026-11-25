# adapter image (spec §15.11): a static Go binary on distroless, built from the
# repository root context because all Go code is one module (spec §17.1).
FROM golang:1.26.11-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY gen/ gen/
COPY adapter/ adapter/
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/adapter ./adapter

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/adapter /usr/local/bin/adapter
EXPOSE 8091
ENTRYPOINT ["/usr/local/bin/adapter"]
CMD ["serve"]
