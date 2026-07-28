#!/bin/sh
set -eu
CACHE_FILE="/var/cache/qnap-monitoring/shared-folders.prom"
if [ -s "${CACHE_FILE}" ]; then
  cat "${CACHE_FILE}"
else
  NOW="$$(date +%s)"
  printf 'qnap_shared_folder_summary,volume=detecting total_bytes=0,used_bytes=0,free_bytes=0,used_percent=0,share_count=0i,failed_count=0i,scan_success=0i,scan_timestamp_seconds=%si\n' "${NOW}"
fi
