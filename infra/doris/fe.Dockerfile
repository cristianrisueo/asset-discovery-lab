# Doris FE image (spec §11.1, §11.2). It starts from the image's default
# fe.conf and replaces only the keys of §11.2. The file stays writable, because
# the entrypoint appends priority_networks at first start.
FROM apache/doris:fe-4.1.5

ARG CONF=/opt/apache-doris/fe/conf/fe.conf
RUN set -eux; \
    grep -q '^JAVA_OPTS_FOR_JDK_17=' "$CONF"; \
    sed -i -E '/^JAVA_OPTS_FOR_JDK_17=/ s/-Xmx[0-9]+[mMgG]/-Xmx1280m/; /^JAVA_OPTS_FOR_JDK_17=/ s/-Xms[0-9]+[mMgG]/-Xms1280m/' "$CONF"; \
    if grep -q '^arrow_flight_sql_port' "$CONF"; then \
      sed -i -E 's/^arrow_flight_sql_port *=.*/arrow_flight_sql_port = 8070/' "$CONF"; \
    else \
      echo 'arrow_flight_sql_port = 8070' >> "$CONF"; \
    fi; \
    grep -E '^arrow_flight_sql_port|^JAVA_OPTS_FOR_JDK_17=' "$CONF" | grep -q -- '-Xmx1280m -Xms1280m'; \
    grep -q '^arrow_flight_sql_port = 8070$' "$CONF"
