#!/bin/bash
set -Eeuo pipefail

mkdir -p \
  /data/cache \
  /data/dashboard \
  /data/grafana/logs \
  /data/grafana/plugins \
  /data/prometheus

gpu_mode="${QNAP_GPU_ENABLED:-false}"
case "${gpu_mode,,}" in
  1|true|yes|on)
    telegraf_config=/opt/qnap/config/qnap-unified-gpu.conf
    dashboard_source=/opt/qnap/dashboards/qnap-gpu.json
    echo "QNAP Monitor One: GPU/PCIe collection enabled" >&2
    ;;
  *)
    telegraf_config=/opt/qnap/config/qnap-unified.conf
    dashboard_source=/opt/qnap/dashboards/qnap-core.json
    echo "QNAP Monitor One: core collection enabled" >&2
    ;;
esac

dashboard_tmp=/data/dashboard/qnap-cn.json.tmp
cp "${dashboard_source}" "${dashboard_tmp}"
mv -f "${dashboard_tmp}" /data/dashboard/qnap-cn.json

. /opt/qnap/scripts/qnap-detect-platform.sh
qnap_detect_platform
echo "QNAP platform=${QNAP_DETECTED_PLATFORM} filesystem=${QNAP_DETECTED_FS} volume=${QNAP_DETECTED_VOLUME_NAME:-not-detected}" >&2

rm -rf /data/cache/disk-refresh.lock /data/cache/refresh.lock

share_interval="${QNAP_SHARE_SCAN_INTERVAL:-3600}"
case "${share_interval}" in
  ''|*[!0-9]*) share_interval=3600 ;;
esac

(
  until /bin/sh /opt/qnap/scripts/qnap-refresh-disks.sh; do sleep 60; done
  while :; do
    sleep 86400
    /bin/sh /opt/qnap/scripts/qnap-refresh-disks.sh || true
  done
) &
disk_worker_pid=$!

(
  until /bin/sh /opt/qnap/scripts/qnap-refresh-shared-folders.sh; do sleep 30; done
  while :; do
    sleep "${share_interval}"
    /bin/sh /opt/qnap/scripts/qnap-refresh-shared-folders.sh || true
  done
) &
share_worker_pid=$!

/usr/local/bin/telegraf --config "${telegraf_config}" &
telegraf_pid=$!

/usr/local/bin/prometheus \
  --config.file=/opt/qnap/config/prometheus.yml \
  --storage.tsdb.path=/data/prometheus \
  --storage.tsdb.retention.time="${PROMETHEUS_RETENTION:-30d}" \
  --web.listen-address=127.0.0.1:9090 &
prometheus_pid=$!

/run.sh &
grafana_pid=$!

shutdown() {
  trap - TERM INT EXIT
  kill -TERM \
    "${grafana_pid}" "${prometheus_pid}" "${telegraf_pid}" \
    "${share_worker_pid}" "${disk_worker_pid}" \
    2>/dev/null || true
  wait || true
}
trap shutdown TERM INT EXIT

set +e
wait -n "${grafana_pid}" "${prometheus_pid}" "${telegraf_pid}"
status=$?
set -e
echo "A required service exited with status ${status}; stopping container" >&2
exit "${status}"
