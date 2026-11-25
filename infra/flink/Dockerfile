# Flink JobManager and TaskManager image (spec §2.3, §10.9, §15.7).
FROM flink:2.1.3-java17

# /flink-data holds checkpoints, savepoints and the label run id. A named
# volume copies this directory, owner included, on first use, so the flink
# user (uid 9999) can write it (spec §10.9, §15.7).
RUN mkdir -p /flink-data/checkpoints /flink-data/savepoints \
 && chown -R 9999:9999 /flink-data

# Structured JSON console logs with a service field (log4j-layout-template-json
# ships in /opt/flink/lib). The rolling file keeps the plain layout for the Flink UI.
COPY --chown=9999:9999 log4j-console.properties /opt/flink/conf/log4j-console.properties
