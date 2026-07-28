#!/bin/sh
# Shared QNAP platform/data-volume detection library.
# It uses feature detection rather than relying only on product names.

qnap_escape_tag() {
  printf '%s' "$$1" | sed 's/\\/\\\\/g; s/ /\\ /g; s/,/\\,/g; s/=/\\=/g'
}

qnap_detect_platform() {
  QNAP_PROC_ROOT="${QNAP_HOST_PROC_ROOT:-/host-proc}"
  QNAP_SYS_ROOT="${QNAP_HOST_SYS_ROOT:-/host-sys}"
  QNAP_CONFIG_ROOT="${QNAP_HOST_CONFIG_ROOT:-/host-etc-config}"
  QNAP_SHARE_ROOT="${QNAP_HOST_SHARE_ROOT:-/share}"
  QNAP_SMB_CONF="${QNAP_CONFIG_ROOT}/smb.conf"

  QNAP_DETECTED_DATA_ROOT=""
  QNAP_DETECTED_VOLUME_NAME=""
  QNAP_DETECTED_FS="unknown"
  QNAP_DETECTED_PLATFORM="unknown"
  QNAP_ARCSTATS=""

  # Optional preferred volume name. If it does not exist, automatic detection continues.
  if [ -n "${QNAP_DATA_VOLUME_NAME:-}" ] && [ -d "${QNAP_SHARE_ROOT}/${QNAP_DATA_VOLUME_NAME}" ]; then
    QNAP_DETECTED_DATA_ROOT="$$(readlink -f "${QNAP_SHARE_ROOT}/${QNAP_DATA_VOLUME_NAME}" 2>/dev/null || true)"
  fi

  # Resolve the real volume root from the first valid QNAP shared-folder path.
  if [ -z "${QNAP_DETECTED_DATA_ROOT}" ] && [ -r "${QNAP_SMB_CONF}" ]; then
    while IFS= read -r share_path; do
      [ -n "${share_path}" ] || continue
      real_path="$$(readlink -f "${share_path}" 2>/dev/null || true)"
      case "${real_path}" in
        "${QNAP_SHARE_ROOT}"/*)
          relative="${real_path#${QNAP_SHARE_ROOT}/}"
          volume_component="${relative%%/*}"
          candidate="${QNAP_SHARE_ROOT}/${volume_component}"
          case "${volume_component}" in
            ZFS*_DATA|CACHEDEV*_DATA)
              if [ -d "${candidate}" ]; then
                QNAP_DETECTED_DATA_ROOT="$$(readlink -f "${candidate}" 2>/dev/null || true)"
                break
              fi
              ;;
          esac
          ;;
      esac
    done <<EOF
$$(awk '
  function trim(s) {
    sub(/^[[:space:]]+/, "", s)
    sub(/[[:space:]\r]+$$/, "", s)
    sub(/^"/, "", s)
    sub(/"$$/, "", s)
    return s
  }
  /^[[:space:]]*[Pp][Aa][Tt][Hh][[:space:]]*=/ {
    p=$$0
    sub(/^[^=]*=[[:space:]]*/, "", p)
    p=trim(p)
    if (p ~ /^\/share\//) print p
  }
' "${QNAP_SMB_CONF}" 2>/dev/null)
EOF
  fi

  # Standard QTS and QuTS hero volume names.
  if [ -z "${QNAP_DETECTED_DATA_ROOT}" ]; then
    for candidate in "${QNAP_SHARE_ROOT}"/ZFS*_DATA "${QNAP_SHARE_ROOT}"/CACHEDEV*_DATA; do
      [ -d "${candidate}" ] || continue
      QNAP_DETECTED_DATA_ROOT="$$(readlink -f "${candidate}" 2>/dev/null || true)"
      [ -n "${QNAP_DETECTED_DATA_ROOT}" ] && break
    done
  fi

  if [ -n "${QNAP_DETECTED_DATA_ROOT}" ]; then
    QNAP_DETECTED_VOLUME_NAME="${QNAP_DETECTED_DATA_ROOT##*/}"
    QNAP_DETECTED_FS="$$(stat -f -c '%T' "${QNAP_DETECTED_DATA_ROOT}" 2>/dev/null || printf 'unknown')"
  fi

  # QNAP QuTS hero may expose ARC under /proc/lpl; standard OpenZFS uses /proc/spl.
  for candidate in \
    "${QNAP_PROC_ROOT}/lpl/kstat/zfs/arcstats" \
    "${QNAP_PROC_ROOT}/spl/kstat/zfs/arcstats" \
    "${QNAP_PROC_ROOT}"/*/kstat/zfs/arcstats; do
    if [ -r "${candidate}" ]; then
      QNAP_ARCSTATS="${candidate}"
      break
    fi
  done

  case "${QNAP_DETECTED_FS}" in
    zfs*|ZFS*) QNAP_DETECTED_PLATFORM="quts_hero" ;;
    ext2*|ext3*|ext4*) QNAP_DETECTED_PLATFORM="qts" ;;
  esac
  if [ -n "${QNAP_ARCSTATS}" ]; then
    QNAP_DETECTED_PLATFORM="quts_hero"
  elif [ "${QNAP_DETECTED_PLATFORM}" = "unknown" ]; then
    # QTS uses an ext-family filesystem, but some stat implementations report a generic name.
    case "${QNAP_DETECTED_VOLUME_NAME}" in
      CACHEDEV*_DATA) QNAP_DETECTED_PLATFORM="qts" ;;
      ZFS*_DATA) QNAP_DETECTED_PLATFORM="quts_hero" ;;
    esac
  fi

  export QNAP_PROC_ROOT QNAP_SYS_ROOT QNAP_CONFIG_ROOT QNAP_SHARE_ROOT QNAP_SMB_CONF
  export QNAP_DETECTED_DATA_ROOT QNAP_DETECTED_VOLUME_NAME QNAP_DETECTED_FS
  export QNAP_DETECTED_PLATFORM QNAP_ARCSTATS
}
