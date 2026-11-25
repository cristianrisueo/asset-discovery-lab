# Doris FE image (spec §11.1, §11.2). It starts from the image's default
# fe.conf and replaces only the keys of §11.2. The file stays writable, because
# the entrypoint appends priority_networks at first start.
FROM apache/doris:fe-4.1.5

ARG CONF=/opt/apache-doris/fe/conf/fe.conf
# §11.2 says 1280m. R9 lowers it to 1024m inside the FE's 1792 MiB budget: the
# live heap is under 200 MB, but G1 touches the whole heap over time, and
# 1280m plus ~680 MB of native memory exceeds the limit (decision D-044).
ARG FE_HEAP=1024m
RUN set -eux; \
    grep -q '^JAVA_OPTS_FOR_JDK_17=' "$CONF"; \
    sed -i -E "/^JAVA_OPTS_FOR_JDK_17=/ s/-Xmx[0-9]+[mMgG]/-Xmx${FE_HEAP}/; /^JAVA_OPTS_FOR_JDK_17=/ s/-Xms[0-9]+[mMgG]/-Xms${FE_HEAP}/" "$CONF"; \
    if grep -q '^arrow_flight_sql_port' "$CONF"; then \
      sed -i -E 's/^arrow_flight_sql_port *=.*/arrow_flight_sql_port = 8070/' "$CONF"; \
    else \
      echo 'arrow_flight_sql_port = 8070' >> "$CONF"; \
    fi; \
    grep -E '^arrow_flight_sql_port|^JAVA_OPTS_FOR_JDK_17=' "$CONF" | grep -q -- "-Xmx${FE_HEAP} -Xms${FE_HEAP}"; \
    grep -q '^arrow_flight_sql_port = 8070$' "$CONF"
