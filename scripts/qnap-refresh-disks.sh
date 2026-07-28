#!/bin/sh
set -eu

CACHE_DIR="/var/cache/qnap-monitoring"
CACHE_FILE="${CACHE_DIR}/disk-inventory.influx"
RAW_FILE="${CACHE_DIR}/disk-inventory.raw.$$"
TMP_FILE="${CACHE_FILE}.tmp.$$"
LOCK_DIR="${CACHE_DIR}/disk-refresh.lock"

mkdir -p "${CACHE_DIR}"
acquire_lock() {
  attempt=0
  while [ "${attempt}" -lt 2 ]; do
    if mkdir "${LOCK_DIR}" 2>/dev/null; then
      printf '%s\n' "$$" > "${LOCK_DIR}/pid"
      return 0
    fi

    lock_pid=""
    if [ -r "${LOCK_DIR}/pid" ]; then
      lock_pid="$$(cat "${LOCK_DIR}/pid" 2>/dev/null || true)"
    fi

    case "${lock_pid}" in
      ''|*[!0-9]*)
        echo "Removing stale disk inventory lock" >&2
        rm -rf "${LOCK_DIR}" 2>/dev/null || true
        ;;
      *)
        if kill -0 "${lock_pid}" 2>/dev/null; then
          echo "Disk inventory refresh is already running with PID ${lock_pid}" >&2
          return 1
        fi
        echo "Removing stale disk inventory lock for PID ${lock_pid}" >&2
        rm -rf "${LOCK_DIR}" 2>/dev/null || true
        ;;
    esac
    attempt=$$((attempt + 1))
  done
  echo "Cannot acquire disk inventory lock" >&2
  return 1
}

if ! acquire_lock; then
  exit 1
fi
cleanup() {
  rm -f "${RAW_FILE}" "${TMP_FILE}"
  rm -rf "${LOCK_DIR}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# --once guarantees an immediate SNMP gather instead of waiting for a
# 24-hour plugin interval. Logs go to stderr; line protocol goes to stdout.
if ! telegraf --once --config /etc/telegraf/qnap-disk-once.conf > "${RAW_FILE}"; then
  echo "Disk inventory SNMP collection failed" >&2
  exit 1
fi

awk '/^qnap_disk_inventory,/ { print }' "${RAW_FILE}" > "${TMP_FILE}"
if [ ! -s "${TMP_FILE}" ]; then
  echo "Disk inventory returned no rows; existing cache is preserved" >&2
  exit 1
fi

mv -f "${TMP_FILE}" "${CACHE_FILE}"
COUNT="$$(wc -l < "${CACHE_FILE}" | tr -d ' ')"
echo "Disk inventory refresh completed: ${COUNT} row(s)" >&2

