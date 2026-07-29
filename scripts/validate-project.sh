#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT INT TERM

echo "Checking shell syntax"
find "${ROOT}" -type f -name '*.sh' -print | while IFS= read -r file; do
  echo "Checking ${file}"
  /bin/sh -n "${file}"
done

echo "Checking unresolved Compose escapes"
if grep -R -nE '\$\$\(|\$\$\{|\$\$[0-9]' \
  "${ROOT}/exporter" \
  "${ROOT}/pcie-exporter" \
  "${ROOT}/process-exporter" \
  "${ROOT}/dashboard-builder" \
  "${ROOT}/prometheus" \
  "${ROOT}/grafana" \
  2>/dev/null
then
  echo "Unresolved Compose escaping detected" >&2
  exit 1
fi

echo "Checking legacy and inconsistent paths"
if grep -R -nE '/usr/local/bin/qnap-|/opt/qnap/qnap-detect-platform\.sh' \
  "${ROOT}/exporter" \
  "${ROOT}/pcie-exporter" \
  "${ROOT}/process-exporter" \
  "${ROOT}/dashboard-builder" \
  "${ROOT}/prometheus" \
  "${ROOT}/grafana" \
  2>/dev/null
then
  echo "Legacy or inconsistent QNAP script path detected" >&2
  exit 1
fi

echo "Checking required project files"
for file in \
  Dockerfile \
  docker-compose.yml \
  docker-compose.dev.yml \
  .env.example \
  .gitignore \
  README.md \
  exporter/config/qnap-snmp.conf \
  exporter/config/qnap-unified.conf \
  exporter/config/qnap-disk-once.conf \
  exporter/scripts/qnap-detect-platform.sh \
  exporter/scripts/qnap-host-memory.sh \
  exporter/scripts/qnap-host-disk.sh \
  exporter/scripts/qnap-host-network.sh \
  exporter/scripts/qnap-refresh-disks.sh \
  exporter/scripts/qnap-read-disk-cache.sh \
  exporter/scripts/qnap-refresh-shared-folders.sh \
  exporter/scripts/qnap-read-shared-cache.sh \
  exporter/scripts/qnap-host-entrypoint.sh \
  pcie-exporter/qnap-pcie.conf \
  pcie-exporter/qnap-pcie-collect.sh \
  process-exporter/qnap-process-exporter.py \
  dashboard-builder/qnap-dashboard-builder.py \
  prometheus/prometheus.yml \
  grafana/provisioning/datasources/prometheus.yml \
  grafana/provisioning/dashboards/qnap.yml \
  grafana/dashboards/qnap-cn.json
do
  if [ ! -s "${ROOT}/${file}" ]; then
    echo "Required file is missing or empty: ${file}" >&2
    exit 1
  fi
done

echo "Checking accidental secret files"
for file in \
  "${ROOT}/.env" \
  "${ROOT}/dockerhub-token.txt" \
  "${ROOT}/github-token.txt"
do
  if [ -f "${file}" ]; then
    echo "Sensitive local file found: ${file}" >&2
    exit 1
  fi
done

if grep -R -nE 'milesxia|milesxia1986|qnap1234|192\.168\.2\.210|192\.168\.100\.138' \
  "${ROOT}" \
  --exclude-dir=.git \
  --exclude='validate-project.sh' \
  --exclude='README.md'
then
  echo "A value from the private v8.6 deployment remains in the project" >&2
  exit 1
fi

echo "Checking Python, JSON and TOML syntax"
PYTHONPYCACHEPREFIX="${TMP_ROOT}/pycache" python3 -m py_compile \
  "${ROOT}/process-exporter/qnap-process-exporter.py" \
  "${ROOT}/dashboard-builder/qnap-dashboard-builder.py"

python3 - "${ROOT}" <<'PY'
import json
import pathlib
import sys

try:
    import tomllib
except ModuleNotFoundError:
    tomllib = None

root = pathlib.Path(sys.argv[1])

dashboard = json.loads((root / "grafana/dashboards/qnap-cn.json").read_text(encoding="utf-8"))
panel_ids = {panel.get("id") for panel in dashboard.get("panels", [])}
required_panels = {50, 51, 52, 53, 54, 55, 60, 61}
missing_panels = required_panels - panel_ids
if missing_panels:
    raise SystemExit(f"Dashboard is missing v8.6 panels: {sorted(missing_panels)}")

if tomllib is not None:
    for relative in (
        "exporter/config/qnap-snmp.conf",
        "exporter/config/qnap-unified.conf",
        "exporter/config/qnap-disk-once.conf",
        "pcie-exporter/qnap-pcie.conf",
    ):
        with (root / relative).open("rb") as handle:
            tomllib.load(handle)
else:
    print("tomllib unavailable; Telegraf will validate TOML during image smoke testing")
PY

echo "Checking SNMP translator and fallback tools"
grep -q 'snmp_translator = "gosmi"' "${ROOT}/exporter/config/qnap-snmp.conf"
grep -q 'snmp_translator = "gosmi"' "${ROOT}/exporter/config/qnap-disk-once.conf"
grep -q 'net-snmp-tools' "${ROOT}/Dockerfile"

echo "Checking Prometheus jobs and preserved metrics"
for job in qnap_exporter qnap_host_exporter qnap_pcie_exporter qnap_process_exporter; do
  grep -q "job_name: ${job}" "${ROOT}/prometheus/prometheus.yml"
done

for metric in \
  qnap_system_cpu_usage \
  qnap_system_uptime_seconds \
  qnap_system_cpu_temperature_celsius \
  qnap_system_system_temperature_celsius \
  qnap_disk_temperature_celsius \
  qnap_fan_speed_rpm \
  qnap_host_memory_used_percent \
  qnap_host_network_receive_bytes_total \
  qnap_host_network_transmit_bytes_total \
  qnap_disk_inventory_capacity_bytes \
  qnap_shared_folder_summary_total_bytes \
  qnap_shared_folder_summary_used_bytes \
  qnap_shared_folder_summary_free_bytes \
  qnap_shared_folder_summary_used_percent
do
  if ! grep -q "${metric}" "${ROOT}/grafana/dashboards/qnap-cn.json"; then
    echo "Dashboard no longer references required v8.6 metric: ${metric}" >&2
    exit 1
  fi
done

echo "Validation completed successfully"
