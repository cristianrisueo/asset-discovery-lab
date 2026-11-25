# syntax=docker/dockerfile:1
# The flink-job image (spec §10.11): Maven builds the shaded job jar, which
# ships in a small image with bash, curl and jq. The submit script is
# infra/flink/submit-job.sh, mounted by compose.

FROM maven:3.9.16-eclipse-temurin-17 AS build
WORKDIR /workspace
COPY pom.xml .
COPY src ./src
# Unit tests run in `make test`; the image build only packages the jar.
RUN --mount=type=cache,target=/root/.m2 mvn -B -q -DskipTests package

FROM alpine:3.24.4
# GNU find and coreutils give sub-second modification times (restore choice).
RUN apk add --no-cache bash curl jq coreutils findutils \
 && addgroup -g 9999 flink \
 && adduser -D -u 9999 -G flink flink \
 && mkdir -p /flink-data /opt/adl \
 && chown 9999:9999 /flink-data
COPY --from=build /workspace/target/processor.jar /opt/adl/processor.jar
USER 9999:9999
ENTRYPOINT ["bash", "/opt/adl/submit-job.sh"]
