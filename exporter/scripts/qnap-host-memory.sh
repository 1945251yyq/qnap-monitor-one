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
MEMINFO="${QNAP_PROC_ROOT}/meminfo"

if [ ! -r "${MEMINFO}" ]; then
  echo "Cannot read host memory data: ${MEMINFO}" >&2
  exit 1
fi

ARC_SIZE=0
ARC_EVICTABLE=0
if [ -n "${QNAP_ARCSTATS}" ] && [ -r "${QNAP_ARCSTATS}" ]; then
  ARC_SIZE="$(awk '$1=="size" {print $3; exit}' "${QNAP_ARCSTATS}" 2>/dev/null || true)"
  ARC_EVICTABLE="$(awk '$1=="evictable" {print $3; exit}' "${QNAP_ARCSTATS}" 2>/dev/null || true)"
fi
case "${ARC_SIZE}" in ''|*[!0-9]*) ARC_SIZE=0 ;; esac
case "${ARC_EVICTABLE}" in ''|*[!0-9]*) ARC_EVICTABLE=0 ;; esac

MODE="linux_generic"
if [ "${QNAP_DETECTED_PLATFORM}" = "qts" ]; then
  MODE="qts"
elif [ "${QNAP_DETECTED_PLATFORM}" = "quts_hero" ] && [ "${ARC_EVICTABLE}" -gt 0 ]; then
  MODE="quts_hero_arc"
elif [ "${QNAP_DETECTED_PLATFORM}" = "quts_hero" ]; then
  MODE="quts_hero_generic"
fi

# All capacity values are emitted as float64-compatible numbers. This avoids
# the 32-bit integer overflow that previously turned RAM values into -2147483648.
awk -v platform="${QNAP_DETECTED_PLATFORM}" \
    -v filesystem="${QNAP_DETECTED_FS}" \
    -v mode="${MODE}" \
    -v arc_size="${ARC_SIZE}" \
    -v arc_evictable="${ARC_EVICTABLE}" '
  /^MemTotal:/ { total=$2*1024 }
  /^MemFree:/ { free=$2*1024 }
  /^Buffers:/ { buffered=$2*1024 }
  /^Cached:/ { cached=$2*1024 }
  /^MemAvailable:/ { kernel_available=$2*1024 }
  END {
    # QTS: QNAP-style used memory excludes free, buffers and page cache.
    # QuTS hero: additionally excludes the evictable part of ZFS ARC, while
    # the non-evictable ARC remains counted as used, matching Resource Monitor.
    reclaimable_arc = (mode == "quts_hero_arc") ? arc_evictable : 0
    available = free + buffered + cached + reclaimable_arc
    used = total - available
    if (used < 0) used=0
    if (used > total) used=total
    available = total - used
    used_percent = (total > 0) ? used*100/total : 0
    arc_display = arc_size - arc_evictable
    if (arc_display < 0) arc_display=0

    printf "qnap_host_memory,platform=%s,mode=%s total=%.0f,available=%.0f,used=%.0f,used_percent=%.6f,free=%.0f,cached=%.0f,buffered=%.0f,kernel_available=%.0f,arc_size=%.0f,arc_evictable=%.0f,arc_display=%.0f\n", platform, mode, total, available, used, used_percent, free, cached, buffered, kernel_available, arc_size, arc_evictable, arc_display
    printf "qnap_host_platform,platform=%s,filesystem=%s info=1\n", platform, filesystem
    printf "qnap_host_memory_collector,mode=%s info=1\n", mode
  }
' "${MEMINFO}"
