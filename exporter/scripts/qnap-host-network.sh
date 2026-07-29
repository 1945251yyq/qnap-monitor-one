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
NET_ROOT="${QNAP_SYS_ROOT}/class/net"
NET_DEV="${QNAP_PROC_ROOT}/net/dev"

[ -d "${NET_ROOT}" ] || { echo "Cannot read host network sysfs" >&2; exit 1; }
[ -r "${NET_DEV}" ] || { echo "Cannot read host network counters" >&2; exit 1; }

emit_interface() {
  iface="$1"
  ifdir="${NET_ROOT}/${iface}"
  counters="$(awk -v dev="${iface}:" '$1==dev {print $2, $10; exit}' "${NET_DEV}" 2>/dev/null || true)"
  set -- ${counters}
  [ "$#" -eq 2 ] || return 0
  rx="$1"; tx="$2"
  speed="$(cat "${ifdir}/speed" 2>/dev/null || printf '0')"
  case "${speed}" in ''|*[!0-9]*|-*) speed=0 ;; esac
  state="$(cat "${ifdir}/operstate" 2>/dev/null || printf 'unknown')"
  if [ "${state}" = "up" ]; then link=1; else link=0; fi
  alias="$(cat "${ifdir}/ifalias" 2>/dev/null || true)"
  [ -n "${alias}" ] || alias="${iface}"
  iface_tag="$(qnap_escape_tag "${iface}")"
  alias_tag="$(qnap_escape_tag "${alias}")"
  speed_tag="$(qnap_escape_tag "${speed}")"
  state_tag="$(qnap_escape_tag "${state}")"
  # Speed and link state are also emitted as labels so one dynamic Grafana panel
  # can show interface identity, negotiated speed, state, upload and download.
  # Byte counters are emitted without an integer suffix to remain float64-safe.
  printf 'qnap_host_network,interface=%s,alias=%s,speed_mbps_label=%s,link_state=%s receive_bytes_total=%s,transmit_bytes_total=%s,speed_mbps=%s,link_up=%si\n' \
    "${iface_tag}" "${alias_tag}" "${speed_tag}" "${state_tag}" "${rx}" "${tx}" "${speed}" "${link}"
  PHYSICAL_COUNT=$((PHYSICAL_COUNT + 1))
}

PHYSICAL_COUNT=0
for ifdir in "${NET_ROOT}"/*; do
  [ -d "${ifdir}" ] || continue
  iface="${ifdir##*/}"
  # A device link in sysfs identifies a hardware-backed interface.
  [ -e "${ifdir}/device" ] || continue
  emit_interface "${iface}"
done

# Driver-specific fallback for QNAP models that do not expose the sysfs device link.
if [ "${PHYSICAL_COUNT}" -eq 0 ]; then
  for ifdir in "${NET_ROOT}"/*; do
    [ -d "${ifdir}" ] || continue
    iface="${ifdir##*/}"
    case "${iface}" in
      eth[0-9]*|en[opsx][0-9]*) emit_interface "${iface}" ;;
    esac
  done
fi

printf 'qnap_host_network_summary physical_count=%si\n' "${PHYSICAL_COUNT}"
