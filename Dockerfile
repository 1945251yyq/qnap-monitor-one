ARG TELEGRAF_VERSION=1.39.2
ARG PROMETHEUS_VERSION=v3.13.0
ARG GRAFANA_VERSION=13.1.1

FROM telegraf:${TELEGRAF_VERSION} AS telegraf
FROM prom/prometheus:${PROMETHEUS_VERSION} AS prometheus

# Ubuntu/glibc is intentional: QNAP's NVIDIA driver tools are glibc binaries.
FROM grafana/grafana:${GRAFANA_VERSION}-ubuntu

USER root

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
       ca-certificates coreutils curl findutils gawk grep sed util-linux \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /opt/qnap/config /opt/qnap/scripts /opt/qnap/dashboards \
       /etc/telegraf /data /var/cache \
    && ln -s /data/cache /var/cache/qnap-monitoring

COPY --from=telegraf /usr/bin/telegraf /usr/local/bin/telegraf
COPY --from=prometheus /bin/prometheus /usr/local/bin/prometheus

COPY config/qnap-unified.conf /opt/qnap/config/qnap-unified.conf
COPY config/qnap-unified-gpu.conf /opt/qnap/config/qnap-unified-gpu.conf
COPY config/qnap-disk-once.conf /etc/telegraf/qnap-disk-once.conf
COPY config/prometheus.yml /opt/qnap/config/prometheus.yml
COPY scripts/ /opt/qnap/scripts/
COPY grafana/dashboards/ /opt/qnap/dashboards/
COPY grafana/provisioning/ /etc/grafana/provisioning/
COPY docker-entrypoint.sh /opt/qnap/docker-entrypoint.sh

RUN chmod 0755 /usr/local/bin/telegraf /usr/local/bin/prometheus \
    /opt/qnap/docker-entrypoint.sh /opt/qnap/scripts/*

ENV GF_PATHS_DATA=/data/grafana \
    GF_PATHS_LOGS=/data/grafana/logs \
    GF_PATHS_PLUGINS=/data/grafana/plugins \
    GF_PATHS_PROVISIONING=/etc/grafana/provisioning \
    GF_DASHBOARDS_DEFAULT_HOME_DASHBOARD_PATH=/data/dashboard/qnap-cn.json

EXPOSE 3000
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=5 \
  CMD curl -fsS http://127.0.0.1:3000/api/health >/dev/null || exit 1

ENTRYPOINT ["/opt/qnap/docker-entrypoint.sh"]
