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

CACHE_DIR="/var/cache/qnap-monitoring"
CACHE_FILE="${CACHE_DIR}/shared-folders.prom"
LOCK_DIR="${CACHE_DIR}/refresh.lock"
SMB_CONF="${QNAP_SMB_CONF}"
SHARE_ROOT="${QNAP_SHARE_ROOT:-/share}"
RAW_LIST="${CACHE_DIR}/shares.raw.$$"
UNIQUE_LIST="${CACHE_DIR}/shares.unique.$$"
SORTED_LIST="${CACHE_DIR}/shares.sorted.$$"
SEEN_LIST="${CACHE_DIR}/shares.seen.$$"
ROOT_LIST="${CACHE_DIR}/shares.roots.$$"
VOLUME_LIST="${CACHE_DIR}/shares.volumes.$$"
STORAGE_LIST="${CACHE_DIR}/shares.storage.$$"
TMP_CACHE="${CACHE_FILE}.tmp.$$"

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
      lock_pid="$(cat "${LOCK_DIR}/pid" 2>/dev/null || true)"
    fi

    case "${lock_pid}" in
      ''|*[!0-9]*)
        echo "Removing stale shared-folder scan lock" >&2
        rm -rf "${LOCK_DIR}" 2>/dev/null || true
        ;;
      *)
        if kill -0 "${lock_pid}" 2>/dev/null; then
          echo "Shared-folder scan is already running with PID ${lock_pid}" >&2
          return 1
        fi
        echo "Removing stale shared-folder scan lock for PID ${lock_pid}" >&2
        rm -rf "${LOCK_DIR}" 2>/dev/null || true
        ;;
    esac
    attempt=$((attempt + 1))
  done
  echo "Cannot acquire shared-folder scan lock" >&2
  return 1
}

if ! acquire_lock; then
  exit 1
fi
cleanup() {
  rm -f "${RAW_LIST}" "${UNIQUE_LIST}" "${SORTED_LIST}" "${SEEN_LIST}" "${ROOT_LIST}" "${VOLUME_LIST}" "${STORAGE_LIST}" "${TMP_CACHE}"
  rm -rf "${LOCK_DIR}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

make_raw_from_smb() {
  : > "${RAW_LIST}"
  [ -r "${SMB_CONF}" ] || return 0
  awk '
    function trim(s) {
      sub(/^[[:space:]]+/, "", s)
      sub(/[[:space:]\r]+$/, "", s)
      sub(/^"/, "", s)
      sub(/"$/, "", s)
      return s
    }
    /^\[/ {
      section=$0
      sub(/^\[/, "", section)
      sub(/\][[:space:]]*$/, "", section)
      next
    }
    /^[[:space:]]*[Pp][Aa][Tt][Hh][[:space:]]*=/ {
      path=$0
      sub(/^[^=]*=[[:space:]]*/, "", path)
      path=trim(path)
      if (section != "" && path ~ /^\/share\//) printf "%s\t%s\n", section, path
    }
  ' "${SMB_CONF}" > "${RAW_LIST}" 2>/dev/null || true
}

make_raw_from_all_volumes() {
  : > "${RAW_LIST}"
  for volume in "${SHARE_ROOT}"/ZFS*_DATA "${SHARE_ROOT}"/CACHEDEV*_DATA; do
    [ -d "${volume}" ] || continue
    find "${volume}" -mindepth 1 -maxdepth 1 -type d ! -name '.*' ! -name '@*' -print 2>/dev/null |
    while IFS= read -r mounted_path; do
      name="${mounted_path##*/}"
      relative="${mounted_path#${SHARE_ROOT}/}"
      printf '%s\t/share/%s\n' "${name}" "${relative}"
    done
  done > "${RAW_LIST}"
}

filter_unique() {
  : > "${UNIQUE_LIST}"
  : > "${SEEN_LIST}"
  TAB="$(printf '\t')"
  while IFS="${TAB}" read -r SHARE_NAME HOST_PATH; do
    [ -n "${SHARE_NAME}" ] || continue
    [ -n "${HOST_PATH}" ] || continue
    case "${HOST_PATH}" in *%*) continue ;; /share/*) ;; *) continue ;; esac

    RELATIVE_PATH="${HOST_PATH#/share/}"
    MOUNT_PATH="${SHARE_ROOT}/${RELATIVE_PATH}"
    REAL_MOUNT_PATH="$(readlink -f "${MOUNT_PATH}" 2>/dev/null || true)"
    [ -n "${REAL_MOUNT_PATH}" ] && [ -d "${REAL_MOUNT_PATH}" ] || continue

    case "${REAL_MOUNT_PATH}" in
      "${SHARE_ROOT}"/ZFS*_DATA|"${SHARE_ROOT}"/ZFS*_DATA/*|"${SHARE_ROOT}"/CACHEDEV*_DATA|"${SHARE_ROOT}"/CACHEDEV*_DATA/*) ;;
      *) continue ;;
    esac

    REAL_RELATIVE="${REAL_MOUNT_PATH#${SHARE_ROOT}/}"
    VOLUME_COMPONENT="${REAL_RELATIVE%%/*}"
    case "${VOLUME_COMPONENT}" in ZFS*_DATA|CACHEDEV*_DATA) ;; *) continue ;; esac
    VOLUME_ROOT="${SHARE_ROOT}/${VOLUME_COMPONENT}"
    CANONICAL_REAL="/share/${REAL_RELATIVE}"

    if grep -Fqx "${REAL_MOUNT_PATH}" "${SEEN_LIST}" 2>/dev/null; then continue; fi
    printf '%s\n' "${REAL_MOUNT_PATH}" >> "${SEEN_LIST}"
    printf '%s\t%s\t%s\t%s\t%s\n' \
      "${SHARE_NAME}" "${HOST_PATH}" "${REAL_MOUNT_PATH}" "${CANONICAL_REAL}" "${VOLUME_ROOT}" >> "${UNIQUE_LIST}"
  done < "${RAW_LIST}"
}

make_raw_from_smb
filter_unique
if [ ! -s "${UNIQUE_LIST}" ]; then
  make_raw_from_all_volumes
  filter_unique
fi

if [ ! -s "${UNIQUE_LIST}" ]; then
  echo "Shared-folder scan found zero valid folders; preserving cache and retrying" >&2
  exit 1
fi

awk -F '\t' '{ print length($3) "\t" $0 }' "${UNIQUE_LIST}" | sort -n | cut -f2- > "${SORTED_LIST}"
cut -f5 "${UNIQUE_LIST}" | sort -u > "${VOLUME_LIST}"
: > "${ROOT_LIST}"

# Build a list of unique underlying storage devices/pools before adding capacity.
# QTS volumes are deduplicated by their actual filesystem source/device.
# QuTS hero datasets are deduplicated by the top-level ZFS pool name, so several
# ZFS*_DATA datasets from the same pool do not multiply the displayed capacity.
: > "${STORAGE_LIST}"
TAB="$(printf '\t')"
while IFS= read -r VOLUME_ROOT; do
  [ -d "${VOLUME_ROOT}" ] || continue

  FS_TYPE="$(stat -f -c '%T' "${VOLUME_ROOT}" 2>/dev/null || printf 'unknown')"
  FS_ID="$(stat -f -c '%i' "${VOLUME_ROOT}" 2>/dev/null || printf 'unknown')"
  DEVICE_ID="$(stat -c '%d' "${VOLUME_ROOT}" 2>/dev/null || printf 'unknown')"

  DF_INFO="$(df -Pk "${VOLUME_ROOT}" 2>/dev/null | awk 'NR==2 {print $1 "\t" $2; exit}' || true)"
  SOURCE=""
  TOTAL_KB=""
  if [ -n "${DF_INFO}" ]; then
    SOURCE="$(printf '%s\n' "${DF_INFO}" | awk -F '\t' 'NR==1 {print $1}')"
    TOTAL_KB="$(printf '%s\n' "${DF_INFO}" | awk -F '\t' 'NR==1 {print $2}')"
  fi

  case "${TOTAL_KB}" in
    ''|*[!0-9]*)
      STATFS="$(stat -f -c '%S %b' "${VOLUME_ROOT}" 2>/dev/null || true)"
      set -- ${STATFS}
      [ "$#" -eq 2 ] || continue
      VOLUME_TOTAL="$(awk -v block="$1" -v count="$2" 'BEGIN { printf "%.0f", block*count }')"
      ;;
    *)
      VOLUME_TOTAL="$(awk -v kb="${TOTAL_KB}" 'BEGIN { printf "%.0f", kb*1024 }')"
      ;;
  esac

  VOLUME_NAME="${VOLUME_ROOT##*/}"
  case "${FS_TYPE}:${VOLUME_NAME}" in
    zfs*:*|ZFS*:*|*:ZFS*_DATA)
      case "${SOURCE}" in
        ''|none|overlay|rootfs)
          STORAGE_KEY="zfs-fsid:${FS_ID}"
          ;;
        */*)
          # ZFS source is normally pool/dataset. Group all datasets by pool.
          ZFS_POOL="${SOURCE%%/*}"
          case "${ZFS_POOL}" in
            ''|dev) STORAGE_KEY="zfs-source:${SOURCE}" ;;
            *) STORAGE_KEY="zfs-pool:${ZFS_POOL}" ;;
          esac
          ;;
        *) STORAGE_KEY="zfs-source:${SOURCE}" ;;
      esac
      ;;
    *)
      case "${SOURCE}" in
        ''|none|overlay|rootfs) STORAGE_KEY="device:${DEVICE_ID}:fsid:${FS_ID}" ;;
        *) STORAGE_KEY="source:${SOURCE}" ;;
      esac
      ;;
  esac

  printf '%s\t%s\t%s\n' "${STORAGE_KEY}" "${VOLUME_TOTAL}" "${VOLUME_ROOT}" >> "${STORAGE_LIST}"
done < "${VOLUME_LIST}"

TOTAL_BYTES="$(awk -F '\t' '
  {
    key=$1
    value=$2+0
    if (!(key in maximum) || value > maximum[key]) maximum[key]=value
  }
  END {
    total=0
    for (key in maximum) total += maximum[key]
    printf "%.0f", total
  }
' "${STORAGE_LIST}")"
UNIQUE_STORAGE_COUNT="$(awk -F '\t' '!seen[$1]++ {count++} END {print count+0}' "${STORAGE_LIST}")"

if [ -z "${TOTAL_BYTES}" ] || [ "${TOTAL_BYTES}" = "0" ]; then
  echo "Cannot read capacity of detected QNAP storage pools/devices" >&2
  exit 1
fi

USED_BYTES=0
SHARE_COUNT=0
SUCCESS_COUNT=0
FAILED_COUNT=0
TAB="$(printf '\t')"
: > "${TMP_CACHE}"

while IFS="${TAB}" read -r SHARE_NAME HOST_PATH REAL_MOUNT_PATH CANONICAL_REAL VOLUME_ROOT; do
  [ -n "${SHARE_NAME}" ] || continue
  INCLUDED="yes"
  while IFS= read -r ROOT_PATH; do
    [ -n "${ROOT_PATH}" ] || continue
    case "${REAL_MOUNT_PATH}" in "${ROOT_PATH}"/*) INCLUDED="no"; break ;; esac
  done < "${ROOT_LIST}"
  if [ "${INCLUDED}" = "yes" ]; then printf '%s\n' "${REAL_MOUNT_PATH}" >> "${ROOT_LIST}"; fi

  SIZE_KB="$(timeout 600 du -sk "${REAL_MOUNT_PATH}" 2>/dev/null | awk 'NR==1 {print $1}' || true)"
  case "${SIZE_KB}" in
    ''|*[!0-9]*)
      SIZE_BYTES=0
      SCAN_OK=0
      SCAN_STATE="abnormal"
      FAILED_COUNT=$((FAILED_COUNT + 1))
      echo "Shared-folder scan failed: ${SHARE_NAME} (${CANONICAL_REAL})" >&2
      ;;
    *)
      SIZE_BYTES="$(awk -v kb="${SIZE_KB}" 'BEGIN { printf "%.0f", kb*1024 }')"
      SCAN_OK=1
      SCAN_STATE="normal"
      SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
      if [ "${INCLUDED}" = "yes" ]; then
        USED_BYTES="$(awk -v a="${USED_BYTES}" -v b="${SIZE_BYTES}" 'BEGIN { printf "%.0f", a+b }')"
      fi
      ;;
  esac

  SHARE_COUNT=$((SHARE_COUNT + 1))
  SHARE_TAG="$(qnap_escape_tag "${SHARE_NAME}")"
  PATH_TAG="$(qnap_escape_tag "${CANONICAL_REAL}")"
  printf 'qnap_shared_folder_usage,share=%s,path=%s,included=%s,scan_state=%s size_bytes=%s,scan_ok=%si\n' \
    "${SHARE_TAG}" "${PATH_TAG}" "${INCLUDED}" "${SCAN_STATE}" "${SIZE_BYTES}" "${SCAN_OK}" >> "${TMP_CACHE}"
done < "${SORTED_LIST}"

if [ "${SHARE_COUNT}" -eq 0 ] || [ "${SUCCESS_COUNT}" -eq 0 ]; then
  echo "Shared-folder scan produced no usable data; preserving cache and retrying" >&2
  exit 1
fi

USED_BYTES="$(awk -v used="${USED_BYTES}" -v total="${TOTAL_BYTES}" 'BEGIN { if (used>total) used=total; printf "%.0f", used }')"
FREE_BYTES="$(awk -v used="${USED_BYTES}" -v total="${TOTAL_BYTES}" 'BEGIN { free=total-used; if (free<0) free=0; printf "%.0f", free }')"
USED_PERCENT="$(awk -v used="${USED_BYTES}" -v total="${TOTAL_BYTES}" 'BEGIN { if (total>0) printf "%.6f", used*100/total; else printf "0" }')"
SCAN_SUCCESS=1
[ "${FAILED_COUNT}" -eq 0 ] || SCAN_SUCCESS=0
SCAN_TIME="$(date +%s)"
printf 'qnap_shared_folder_summary,volume=all total_bytes=%s,used_bytes=%s,free_bytes=%s,used_percent=%s,share_count=%si,failed_count=%si,scan_success=%si,scan_timestamp_seconds=%si\n' \
  "${TOTAL_BYTES}" "${USED_BYTES}" "${FREE_BYTES}" "${USED_PERCENT}" "${SHARE_COUNT}" "${FAILED_COUNT}" "${SCAN_SUCCESS}" "${SCAN_TIME}" >> "${TMP_CACHE}"

mv -f "${TMP_CACHE}" "${CACHE_FILE}"
echo "Shared-folder scan completed: ${SHARE_COUNT} folders, ${UNIQUE_STORAGE_COUNT} unique storage pool(s)/device(s), ${FAILED_COUNT} failed" >&2
