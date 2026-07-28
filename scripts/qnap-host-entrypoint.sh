#!/bin/sh
set -eu

INTERVAL="${QNAP_SHARE_SCAN_INTERVAL:-3600}"
case "${INTERVAL}" in ''|*[!0-9]*) INTERVAL=3600 ;; esac

. /opt/qnap/qnap-detect-platform.sh
qnap_detect_platform
echo "QNAP platform=${QNAP_DETECTED_PLATFORM} filesystem=${QNAP_DETECTED_FS} volume=${QNAP_DETECTED_VOLUME_NAME:-not-detected} arcstats=${QNAP_ARCSTATS:-none}" >&2

# A recreated container can inherit lock directories from the persistent
# cache volume even though the processes that created them no longer exist.
# At this point no refresh worker has been started in this container, so
# both startup locks are safe to remove before launching the workers.
rm -rf \
  /var/cache/qnap-monitoring/disk-refresh.lock \
  /var/cache/qnap-monitoring/refresh.lock \
  2>/dev/null || true

(
  # Identify disks immediately after container startup. If SNMP is not ready,
  # retry every 60 seconds without deleting the last valid cache. After the
  # first successful run, refresh disk health once every 24 hours.
  until /bin/sh /opt/qnap/qnap-refresh-disks.sh; do
    sleep 60
  done
  while :; do
    sleep 86400
    /bin/sh /opt/qnap/qnap-refresh-disks.sh || true
  done
) &

(
  # Run the first scan immediately. Empty/invalid results never overwrite a
  # prior valid cache and are retried every 30 seconds until data appears.
  until /bin/sh /opt/qnap/qnap-refresh-shared-folders.sh; do
    sleep 30
  done
  while :; do
    sleep "${INTERVAL}"
    /bin/sh /opt/qnap/qnap-refresh-shared-folders.sh || true
  done
) &

exec telegraf --config /etc/telegraf/telegraf.conf
