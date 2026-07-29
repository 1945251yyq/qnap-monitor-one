#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
PLATFORM_LIBRARY="${SCRIPT_DIR}/qnap-detect-platform.sh"
if [ ! -r "${PLATFORM_LIBRARY}" ]; then
  echo "Missing platform detection library: ${PLATFORM_LIBRARY}" >&2
  exit 1
fi
. "${PLATFORM_LIBRARY}"
qnap_detect_platform
TARGET="${QNAP_DETECTED_DATA_ROOT}"

if [ -z "${TARGET}" ] || [ ! -d "${TARGET}" ]; then
  echo "Cannot auto-detect QNAP data volume" >&2
  exit 1
fi

STATFS="$(stat -f -c '%S %b' "${TARGET}" 2>/dev/null || true)"
set -- ${STATFS}
if [ "$#" -ne 2 ]; then
  echo "Cannot read data-volume capacity: ${TARGET}" >&2
  exit 1
fi
TOTAL_BYTES="$(awk -v block="$1" -v count="$2" 'BEGIN { printf "%.0f", block*count }')"
VOLUME_TAG="$(qnap_escape_tag "${QNAP_DETECTED_VOLUME_NAME}")"
FS_TAG="$(qnap_escape_tag "${QNAP_DETECTED_FS}")"
printf 'qnap_host_disk,volume=%s,filesystem=%s total=%s\n' "${VOLUME_TAG}" "${FS_TAG}" "${TOTAL_BYTES}"
