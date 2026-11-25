# Doris BE image (spec §11.1, §11.2). It starts from the image's default
# be.conf and replaces or appends only the keys of §11.2. The file stays
# writable, because the entrypoint appends priority_networks at first start.
FROM apache/doris:be-4.1.5

ARG CONF=/opt/apache-doris/be/conf/be.conf
RUN set -eux; \
    grep -q '^JAVA_OPTS_FOR_JDK_17=' "$CONF"; \
    sed -i -E '/^JAVA_OPTS_FOR_JDK_17=/ s/-Xmx[0-9]+[mMgG]/-Xmx512m/' "$CONF"; \
    if grep -q '^arrow_flight_sql_port' "$CONF"; then \
      sed -i -E 's/^arrow_flight_sql_port *=.*/arrow_flight_sql_port = 8050/' "$CONF"; \
    else \
      echo 'arrow_flight_sql_port = 8050' >> "$CONF"; \
    fi; \
    if grep -q '^mem_limit' "$CONF"; then \
      sed -i -E 's/^mem_limit *=.*/mem_limit = 2048M/' "$CONF"; \
    else \
      echo 'mem_limit = 2048M' >> "$CONF"; \
    fi; \
    grep -q -- '-Xmx512m' "$CONF"; \
    grep -q '^arrow_flight_sql_port = 8050$' "$CONF"; \
    grep -q '^mem_limit = 2048M$' "$CONF"
