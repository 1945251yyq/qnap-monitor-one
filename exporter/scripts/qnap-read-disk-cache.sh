#!/bin/sh
set -eu
CACHE_FILE="/var/cache/qnap-monitoring/disk-inventory.influx"
if [ -s "${CACHE_FILE}" ]; then
  cat "${CACHE_FILE}"
fi

