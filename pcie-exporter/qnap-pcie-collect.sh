#!/bin/sh
set -u

SYS_ROOT="${QNAP_HOST_SYS_ROOT:-/host-sys}"
PROC_ROOT="${QNAP_HOST_PROC_ROOT:-/host-proc}"
CONFIG_ROOT="${QNAP_HOST_CONFIG_ROOT:-/host-etc-config}"
CACHE_ROOT="${QNAP_PCIE_CACHE_ROOT:-/tmp/qnap-pcie}"
PCI_ROOT="$SYS_ROOT/bus/pci/devices"
SLOT_ROOT="$SYS_ROOT/bus/pci/slots"

# PCIe cache is transient. QNAP named volumes may retain restrictive ownership,
# so always verify writability and fall back to the container's /tmp.
mkdir -p "$CACHE_ROOT" 2>/dev/null || true
if [ ! -d "$CACHE_ROOT" ] || [ ! -w "$CACHE_ROOT" ]; then
  CACHE_ROOT="/tmp/qnap-pcie"
  mkdir -p "$CACHE_ROOT" 2>/dev/null || {
    echo 'qnap_pcie_collector_status collector_up=0i'
    exit 0
  }
fi
[ -d "$PCI_ROOT" ] || { echo 'qnap_pcie_collector_status collector_up=0i'; exit 0; }

escape_tag() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/ /\\ /g; s/,/\\,/g; s/=/\\=/g'
}

trim() {
  printf '%s' "$1" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

is_number() {
  case "${1:-}" in
    ''|N/A|n/a|Not\ Supported|*'['*|*[!0-9.+-]*) return 1 ;;
    *) return 0 ;;
  esac
}

num_or_zero() {
  if is_number "${1:-}"; then printf '%s' "$1"; else printf '0'; fi
}

vendor_name() {
  case "$1" in
    10de) printf 'NVIDIA' ;;
    1002|1022) printf 'AMD' ;;
    8086) printf 'Intel' ;;
    15b3) printf 'NVIDIA-Mellanox' ;;
    14e4) printf 'Broadcom' ;;
    1000) printf 'Broadcom-LSI' ;;
    1b4b|11ab) printf 'Marvell' ;;
    1d6a) printf 'Aquantia' ;;
    10ec) printf 'Realtek' ;;
    1c5c) printf 'SK hynix' ;;
    144d) printf 'Samsung' ;;
    1987) printf 'Phison' ;;
    1e60) printf 'Hailo' ;;
    1ac1) printf 'Google' ;;
    *) printf 'PCI-%s' "$1" ;;
  esac
}

category_name() {
  case "$1" in
    01) printf '存储控制器' ;;
    02) printf '网卡' ;;
    03) printf '显卡' ;;
    04) printf '多媒体设备' ;;
    05) printf '内存控制器' ;;
    08) printf '系统外设' ;;
    0c) printf '总线控制器' ;;
    10) printf '加密设备' ;;
    12) printf 'AI加速器' ;;
    13) printf '仪器设备' ;;
    *) printf '其他设备' ;;
  esac
}

allowed_class() {
  # Keep device classes that can represent actual expansion hardware.
  # Chipset memory/system/bus controllers are intentionally excluded.
  case "$1" in 01|02|03|04|10|12|13) return 0 ;; *) return 1 ;; esac
}

slot_for_bdf() {
  bdf="$1"
  devdir="$2"
  if [ -r "$devdir/physical_slot" ]; then
    slot="$(cat "$devdir/physical_slot" 2>/dev/null || true)"
    [ -n "$slot" ] && { printf '%s' "$slot"; return; }
  fi
  if [ -d "$SLOT_ROOT" ]; then
    for sdir in "$SLOT_ROOT"/*; do
      [ -d "$sdir" ] || continue
      [ -r "$sdir/address" ] || continue
      addr="$(cat "$sdir/address" 2>/dev/null || true)"
      case "$bdf" in
        "$addr"|"$addr".*) printf '%s' "${sdir##*/}"; return ;;
      esac
    done
  fi
  printf '未标记'
}

driver_for() {
  if [ -L "$1/driver" ]; then basename "$(readlink -f "$1/driver" 2>/dev/null || true)"; else printf '未加载'; fi
}

has_net_interface() {
  bdf="$1"
  for ifdir in "$SYS_ROOT"/class/net/*; do
    [ -d "$ifdir" ] || continue
    [ -e "$ifdir/device" ] || continue
    [ "$(basename "$(readlink -f "$ifdir/device" 2>/dev/null || true)")" = "$bdf" ] && return 0
  done
  return 1
}

has_nvme_controller() {
  bdf="$1"
  for ctrlpath in "$SYS_ROOT"/class/nvme/nvme*; do
    [ -d "$ctrlpath" ] || continue
    [ "$(basename "$(readlink -f "$ctrlpath/device" 2>/dev/null || true)")" = "$bdf" ] && return 0
  done
  return 1
}

has_video_node() {
  bdf="$1"
  for vnode in "$SYS_ROOT"/class/video4linux/video*; do
    [ -e "$vnode/device" ] || continue
    vpath="$(readlink -f "$vnode/device" 2>/dev/null || true)"
    case "$vpath" in *"/$bdf"|*"/$bdf/"*) return 0 ;; esac
  done
  return 1
}

list_contains_bdf() {
  list="$(printf '%s' "${1:-}" | tr ',' ' ')"
  target="$(printf '%s' "$2" | tr 'A-F' 'a-f')"
  for item in $list; do
    item="$(printf '%s' "$item" | tr 'A-F' 'a-f')"
    [ "$item" = "$target" ] && return 0
  done
  return 1
}

has_explicit_physical_slot() {
  devdir="$1"
  [ -r "$devdir/physical_slot" ] || return 1
  slot_value="$(cat "$devdir/physical_slot" 2>/dev/null || true)"
  [ -n "$slot_value" ]
}

nvidia_proc_present() {
  bdf="$1"
  [ -d "$PROC_ROOT/driver/nvidia/gpus/$bdf" ] && return 0
  [ -d "$PROC_ROOT/driver/nvidia/gpus/00000000:${bdf#0000:}" ] && return 0
  return 1
}

drm_has_dedicated_vram() {
  bdf="$1"
  for carddev in "$SYS_ROOT"/class/drm/card[0-9]*/device; do
    [ -e "$carddev" ] || continue
    [ "$(basename "$(readlink -f "$carddev" 2>/dev/null || true)")" = "$bdf" ] || continue
    [ -r "$carddev/mem_info_vram_total" ] || continue
    vram="$(cat "$carddev/mem_info_vram_total" 2>/dev/null || true)"
    case "$vram" in ''|*[!0-9]*) continue ;; esac
    [ "$vram" -gt 0 ] && return 0
  done
  return 1
}

link_text() {
  speed="$(cat "$1/current_link_speed" 2>/dev/null || true)"
  width="$(cat "$1/current_link_width" 2>/dev/null || true)"
  if [ -n "$speed" ] || [ -n "$width" ]; then
    printf '%s x%s' "${speed:-未知}" "${width:-?}"
  else
    printf '未提供'
  fi
}

first_hwmon() {
  devdir="$1"
  for h in "$devdir"/hwmon/hwmon* "$devdir"/*/hwmon/hwmon*; do
    [ -d "$h" ] || continue
    printf '%s' "$h"
    return
  done
}

read_millivalue() {
  f="$1"
  [ -r "$f" ] || return 1
  v="$(cat "$f" 2>/dev/null || true)"
  case "$v" in ''|*[!0-9-]*) return 1 ;; esac
  awk -v v="$v" 'BEGIN { printf "%.3f", v/1000 }'
}

read_microwatts() {
  f="$1"
  [ -r "$f" ] || return 1
  v="$(cat "$f" 2>/dev/null || true)"
  case "$v" in ''|*[!0-9-]*) return 1 ;; esac
  awk -v v="$v" 'BEGIN { printf "%.3f", v/1000000 }'
}

# Locate QNAP NVIDIA GPU Driver package without assuming CACHEDEV/ZFS volume names.
get_qpkg_path() {
  name="$1"
  conf="$CONFIG_ROOT/qpkg.conf"
  [ -r "$conf" ] || return 1
  awk -v section="[$name]" '
    $0 == section { in_section=1; next }
    /^\[/ { in_section=0 }
    in_section && /^[[:space:]]*Install_Path[[:space:]]*=/ {
      line=$0; sub(/^[^=]*=[[:space:]]*/, "", line); gsub(/[\r\"]/, "", line); print line; exit
    }
  ' "$conf"
}

NVIDIA_FILE="$CACHE_ROOT/nvidia-current.csv"
rm -f "$NVIDIA_FILE" "$NVIDIA_FILE.tmp" 2>/dev/null || true
if ! touch "$NVIDIA_FILE" 2>/dev/null; then
  # NVIDIA metrics are optional. Never abort generic PCIe collection because
  # a temporary cache file cannot be created.
  NVIDIA_FILE="/tmp/qnap-pcie-nvidia-current.csv"
  rm -f "$NVIDIA_FILE" "$NVIDIA_FILE.tmp" 2>/dev/null || true
  touch "$NVIDIA_FILE" 2>/dev/null || NVIDIA_FILE=""
fi
NVIDIA_PATH=""
NVIDIA_PKG="$(get_qpkg_path NVIDIA_GPU_DRV 2>/dev/null || true)"
if [ -n "$NVIDIA_PKG" ]; then
  for p in \
    "$NVIDIA_PKG/usr/lib/bin/nvidia-smi" \
    "$NVIDIA_PKG/usr/bin/nvidia-smi" \
    "$NVIDIA_PKG/usr/nvidia/bin/nvidia-smi"; do
    [ -x "$p" ] || continue
    NVIDIA_PATH="$p"
    break
  done
fi

if [ -n "$NVIDIA_PATH" ] && [ -n "$NVIDIA_FILE" ]; then
  NVIDIA_LIB="$NVIDIA_PKG/usr/lib:$NVIDIA_PKG/usr/lib64:$NVIDIA_PKG/usr/nvidia/lib64"
  if command -v timeout >/dev/null 2>&1; then
    timeout 8 env LD_LIBRARY_PATH="$NVIDIA_LIB" "$NVIDIA_PATH" \
      --query-gpu=pci.bus_id,name,utilization.gpu,memory.total,memory.used,temperature.gpu,fan.speed,power.draw,utilization.encoder,utilization.decoder \
      --format=csv,noheader,nounits > "$NVIDIA_FILE.tmp" 2>/dev/null || true
  else
    env LD_LIBRARY_PATH="$NVIDIA_LIB" "$NVIDIA_PATH" \
      --query-gpu=pci.bus_id,name,utilization.gpu,memory.total,memory.used,temperature.gpu,fan.speed,power.draw,utilization.encoder,utilization.decoder \
      --format=csv,noheader,nounits > "$NVIDIA_FILE.tmp" 2>/dev/null || true
  fi
  if [ -s "$NVIDIA_FILE.tmp" ]; then
    awk -F',' '
      function trim(s){gsub(/^[ \t]+|[ \t]+$/, "", s); return s}
      {
        bus=tolower(trim($1)); sub(/^00000000:/, "0000:", bus)
        printf "%s|%s|%s|%s|%s|%s|%s|%s|%s|%s\n", bus,trim($2),trim($3),trim($4),trim($5),trim($6),trim($7),trim($8),trim($9),trim($10)
      }
    ' "$NVIDIA_FILE.tmp" > "$NVIDIA_FILE"
  fi
  rm -f "$NVIDIA_FILE.tmp"
fi

find_nvidia_row() {
  bdf="$(printf '%s' "$1" | tr 'A-F' 'a-f')"
  [ -n "$NVIDIA_FILE" ] || return 0
  awk -F'|' -v b="$bdf" '$1==b {print; exit}' "$NVIDIA_FILE" 2>/dev/null || true
}

model_from_nvidia_proc() {
  bdf="$1"
  for info in "$PROC_ROOT/driver/nvidia/gpus/$bdf/information" "$PROC_ROOT/driver/nvidia/gpus/00000000:${bdf#0000:}/information"; do
    [ -r "$info" ] || continue
    awk -F: '/^Model:/ {sub(/^[[:space:]]*/, "", $2); print $2; exit}' "$info"
    return
  done
}

TOTAL=0
GPU_COUNT=0
NIC_COUNT=0
STORAGE_COUNT=0

for devdir in "$PCI_ROOT"/*; do
  [ -d "$devdir" ] || continue
  bdf="${devdir##*/}"
  class_raw="$(cat "$devdir/class" 2>/dev/null || true)"
  class_hex="${class_raw#0x}"
  base_class="$(printf '%s' "$class_hex" | cut -c1-2 | tr 'A-F' 'a-f')"
  class_group="$(printf '%s' "$class_hex" | cut -c1-4 | tr 'A-F' 'a-f')"
  allowed_class "$base_class" || continue
  # 0403 is normally an onboard/audio function or a GPU HDMI-audio function,
  # not a separate expansion device that needs its own monitoring row.
  [ "$class_group" = '0403' ] && continue

  vendor_id="$(cat "$devdir/vendor" 2>/dev/null | sed 's/^0x//' | tr 'A-F' 'a-f')"
  device_id="$(cat "$devdir/device" 2>/dev/null | sed 's/^0x//' | tr 'A-F' 'a-f')"
  vendor="$(vendor_name "$vendor_id")"
  category="$(category_name "$base_class")"
  slot="$(slot_for_bdf "$bdf" "$devdir")"
  driver="$(driver_for "$devdir")"
  link="$(link_text "$devdir")"

  # QNAP 的 /sys/bus/pci/slots 可能同时列出板载网卡、SATA 控制器等
  # 下游端点，因此槽位目录只能用于显示标签，不能单独证明设备是扩展卡。
  # 严格确认顺序：手动排除 > 手动包含 > physical_slot > 厂商/驱动专用接口。
  is_expansion=0
  if list_contains_bdf "${QNAP_PCIE_EXCLUDE_BDFS:-}" "$bdf"; then
    is_expansion=0
  elif list_contains_bdf "${QNAP_PCIE_INCLUDE_BDFS:-}" "$bdf"; then
    is_expansion=1
  elif has_explicit_physical_slot "$devdir"; then
    is_expansion=1
  else
    case "$base_class" in
      03|12)
        # NVIDIA 使用 /proc/driver/nvidia/gpus 中的实际 PCI 地址确认；
        # AMD 等设备仅在 DRM 明确暴露独立显存时自动确认。
        if nvidia_proc_present "$bdf" || drm_has_dedicated_vram "$bdf"; then
          is_expansion=1
        fi
        ;;
      *)
        # 网卡、存储控制器、多媒体卡等在缺少 physical_slot 时无法
        # 可靠区分板载设备与插卡，默认不展示；可通过 INCLUDE_BDFS 明确加入。
        is_expansion=0
        ;;
    esac
  fi
  [ "$is_expansion" -eq 1 ] || continue
  model="$vendor 0x$device_id"
  capability='仅设备识别'
  status='已连接'
  [ "$driver" = '未加载' ] && status='驱动未加载'

  nrow=''
  if [ "$base_class" = '03' ] && [ "$vendor_id" = '10de' ]; then
    nrow="$(find_nvidia_row "$bdf")"
    if [ -n "$nrow" ]; then
      model="$(printf '%s' "$nrow" | awk -F'|' '{print $2}')"
      capability='完整监控'
    else
      pmodel="$(model_from_nvidia_proc "$bdf")"
      [ -n "$pmodel" ] && model="$pmodel"
      capability='基础监控'
    fi
  fi

  # Standard DRM metrics (primarily AMD; also future-proof for drivers exposing the same ABI).
  gpu_util=''; vram_total=''; vram_used=''
  if [ "$base_class" = '03' ] || [ "$base_class" = '12' ]; then
    for carddev in "$SYS_ROOT"/class/drm/card[0-9]*/device; do
      [ -e "$carddev" ] || continue
      [ "$(basename "$(readlink -f "$carddev" 2>/dev/null || true)")" = "$bdf" ] || continue
      [ -r "$carddev/gpu_busy_percent" ] && gpu_util="$(cat "$carddev/gpu_busy_percent" 2>/dev/null || true)"
      [ -r "$carddev/mem_info_vram_total" ] && vram_total="$(cat "$carddev/mem_info_vram_total" 2>/dev/null || true)"
      [ -r "$carddev/mem_info_vram_used" ] && vram_used="$(cat "$carddev/mem_info_vram_used" 2>/dev/null || true)"
      [ -n "$gpu_util$vram_total$vram_used" ] && capability='完整监控'
      break
    done
  fi

  # Generic hwmon data works across GPU, NIC, NVMe and controller drivers when exposed.
  temperature=''; fan_rpm=''; power_watts=''
  hmon="$(first_hwmon "$devdir")"
  if [ -n "$hmon" ]; then
    for tf in "$hmon"/temp*_input; do [ -r "$tf" ] || continue; temperature="$(read_millivalue "$tf" || true)"; [ -n "$temperature" ] && break; done
    for ff in "$hmon"/fan*_input; do [ -r "$ff" ] || continue; fan_rpm="$(cat "$ff" 2>/dev/null || true)"; [ -n "$fan_rpm" ] && break; done
    for pf in "$hmon"/power*_average "$hmon"/power*_input; do [ -r "$pf" ] || continue; power_watts="$(read_microwatts "$pf" || true)"; [ -n "$power_watts" ] && break; done
    [ -n "$temperature$fan_rpm$power_watts" ] && [ "$capability" = '仅设备识别' ] && capability='基础监控'
  fi

  # NVIDIA values override generic fields when QNAP nvidia-smi is executable.
  if [ -n "$nrow" ]; then
    gpu_util="$(printf '%s' "$nrow" | awk -F'|' '{print $3}')"
    mem_total_mb="$(printf '%s' "$nrow" | awk -F'|' '{print $4}')"
    mem_used_mb="$(printf '%s' "$nrow" | awk -F'|' '{print $5}')"
    ntemp="$(printf '%s' "$nrow" | awk -F'|' '{print $6}')"
    fan_percent="$(printf '%s' "$nrow" | awk -F'|' '{print $7}')"
    npower="$(printf '%s' "$nrow" | awk -F'|' '{print $8}')"
    enc="$(printf '%s' "$nrow" | awk -F'|' '{print $9}')"
    dec="$(printf '%s' "$nrow" | awk -F'|' '{print $10}')"
    is_number "$mem_total_mb" && vram_total="$(awk -v v="$mem_total_mb" 'BEGIN{printf "%.0f",v*1048576}')"
    is_number "$mem_used_mb" && vram_used="$(awk -v v="$mem_used_mb" 'BEGIN{printf "%.0f",v*1048576}')"
    is_number "$ntemp" && temperature="$ntemp"
    is_number "$npower" && power_watts="$npower"
  else
    fan_percent=''; enc=''; dec=''
  fi

  bdf_t="$(escape_tag "$bdf")"; slot_t="$(escape_tag "$slot")"; category_t="$(escape_tag "$category")"
  vendor_t="$(escape_tag "$vendor")"; model_t="$(escape_tag "$model")"; driver_t="$(escape_tag "$driver")"
  capability_t="$(escape_tag "$capability")"; status_t="$(escape_tag "$status")"; link_t="$(escape_tag "$link")"

  printf 'qnap_pcie_device_snapshot,bdf=%s,slot=%s,category=%s,vendor=%s,model=%s,driver=%s,capability=%s,status=%s,link=%s,is_expansion=1 present=1i\n' \
    "$bdf_t" "$slot_t" "$category_t" "$vendor_t" "$model_t" "$driver_t" "$capability_t" "$status_t" "$link_t"

  if [ -n "$temperature$fan_rpm$power_watts" ]; then
    fields='present=1i'
    is_number "$temperature" && fields="$fields,temperature_celsius=$temperature"
    is_number "$fan_rpm" && fields="$fields,fan_rpm=$fan_rpm"
    is_number "$power_watts" && fields="$fields,power_watts=$power_watts"
    printf 'qnap_pcie_sensor,bdf=%s,slot=%s,category=%s,model=%s,is_expansion=1 %s\n' "$bdf_t" "$slot_t" "$category_t" "$model_t" "$fields"
  fi

  if [ "$base_class" = '03' ] || [ "$base_class" = '12' ]; then
    fields='present=1i'
    is_number "$gpu_util" && fields="$fields,utilization_percent=$gpu_util"
    is_number "$vram_total" && fields="$fields,memory_total_bytes=$vram_total"
    is_number "$vram_used" && fields="$fields,memory_used_bytes=$vram_used"
    if is_number "$vram_total" && is_number "$vram_used" && awk -v t="$vram_total" 'BEGIN{exit !(t>0)}'; then
      mem_pct="$(awk -v u="$vram_used" -v t="$vram_total" 'BEGIN{printf "%.3f",100*u/t}')"
      fields="$fields,memory_usage_percent=$mem_pct"
    fi
    is_number "$temperature" && fields="$fields,temperature_celsius=$temperature"
    is_number "$fan_percent" && fields="$fields,fan_percent=$fan_percent"
    is_number "$power_watts" && fields="$fields,power_watts=$power_watts"
    is_number "$enc" && fields="$fields,encoder_percent=$enc"
    is_number "$dec" && fields="$fields,decoder_percent=$dec"
    printf 'qnap_pcie_gpu,bdf=%s,slot=%s,model=%s,vendor=%s,is_expansion=1 %s\n' "$bdf_t" "$slot_t" "$model_t" "$vendor_t" "$fields"
    GPU_COUNT=$((GPU_COUNT + 1))
  fi

  if [ "$base_class" = '02' ]; then
    found_nic=0
    for ifdir in "$SYS_ROOT"/class/net/*; do
      [ -d "$ifdir" ] || continue
      [ -e "$ifdir/device" ] || continue
      [ "$(basename "$(readlink -f "$ifdir/device" 2>/dev/null || true)")" = "$bdf" ] || continue
      iface="${ifdir##*/}"
      rx="$(cat "$ifdir/statistics/rx_bytes" 2>/dev/null || printf 0)"
      tx="$(cat "$ifdir/statistics/tx_bytes" 2>/dev/null || printf 0)"
      rxerr="$(cat "$ifdir/statistics/rx_errors" 2>/dev/null || printf 0)"
      txerr="$(cat "$ifdir/statistics/tx_errors" 2>/dev/null || printf 0)"
      rxd="$(cat "$ifdir/statistics/rx_dropped" 2>/dev/null || printf 0)"
      txd="$(cat "$ifdir/statistics/tx_dropped" 2>/dev/null || printf 0)"
      speed="$(cat "$ifdir/speed" 2>/dev/null || printf 0)"; case "$speed" in ''|*[!0-9]*|-*) speed=0;; esac
      state="$(cat "$ifdir/operstate" 2>/dev/null || printf unknown)"; [ "$state" = up ] && up=1 || up=0
      iface_t="$(escape_tag "$iface")"; state_t="$(escape_tag "$state")"
      printf 'qnap_pcie_nic,bdf=%s,slot=%s,model=%s,interface=%s,link_state=%s,is_expansion=1 receive_bytes_total=%s,transmit_bytes_total=%s,receive_errors_total=%s,transmit_errors_total=%s,receive_dropped_total=%s,transmit_dropped_total=%s,speed_mbps=%s,link_up=%si\n' \
        "$bdf_t" "$slot_t" "$model_t" "$iface_t" "$state_t" "$rx" "$tx" "$rxerr" "$txerr" "$rxd" "$txd" "$speed" "$up"
      found_nic=1
    done
    [ "$found_nic" -eq 1 ] && NIC_COUNT=$((NIC_COUNT + 1))
  fi

  if [ "$base_class" = '01' ]; then
    found_storage=0
    for ctrlpath in "$SYS_ROOT"/class/nvme/nvme*; do
      [ -d "$ctrlpath" ] || continue
      [ "$(basename "$(readlink -f "$ctrlpath/device" 2>/dev/null || true)")" = "$bdf" ] || continue
      ctrl="${ctrlpath##*/}"
      ctrl_model="$(trim "$(cat "$ctrlpath/model" 2>/dev/null || printf '%s' "$model")")"
      ctrl_model_t="$(escape_tag "$ctrl_model")"
      ctrl_temp=''
      for hf in "$ctrlpath"/device/hwmon/hwmon*/temp*_input "$ctrlpath"/hwmon/hwmon*/temp*_input; do
        [ -r "$hf" ] || continue
        ctrl_temp="$(read_millivalue "$hf" || true)"; [ -n "$ctrl_temp" ] && break
      done
      for blk in "$SYS_ROOT"/class/block/${ctrl}n*; do
        [ -d "$blk" ] || continue
        [ -r "$blk/partition" ] && continue
        dev="${blk##*/}"
        sectors="$(cat "$blk/size" 2>/dev/null || printf 0)"
        capacity="$(awk -v s="$sectors" 'BEGIN{printf "%.0f",s*512}')"
        statline="$(cat "$blk/stat" 2>/dev/null || true)"
        read_sectors="$(printf '%s' "$statline" | awk '{print $3+0}')"
        write_sectors="$(printf '%s' "$statline" | awk '{print $7+0}')"
        read_bytes="$(awk -v s="$read_sectors" 'BEGIN{printf "%.0f",s*512}')"
        write_bytes="$(awk -v s="$write_sectors" 'BEGIN{printf "%.0f",s*512}')"
        dev_t="$(escape_tag "$dev")"
        fields="capacity_bytes=$capacity,read_bytes_total=$read_bytes,write_bytes_total=$write_bytes,present=1i"
        is_number "$ctrl_temp" && fields="$fields,temperature_celsius=$ctrl_temp"
        printf 'qnap_pcie_storage,bdf=%s,slot=%s,model=%s,device=%s,is_expansion=1 %s\n' "$bdf_t" "$slot_t" "$ctrl_model_t" "$dev_t" "$fields"
        found_storage=1
      done
    done
    [ "$found_storage" -eq 1 ] && STORAGE_COUNT=$((STORAGE_COUNT + 1))
  fi

  TOTAL=$((TOTAL + 1))
done

printf 'qnap_pcie_summary devices=%si,gpu_devices=%si,nic_devices=%si,storage_devices=%si,collector_up=1i\n' "$TOTAL" "$GPU_COUNT" "$NIC_COUNT" "$STORAGE_COUNT"
