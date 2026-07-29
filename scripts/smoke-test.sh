#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP_ROOT="$(mktemp -d)"
COMPOSE_FILES="-f ${ROOT}/docker-compose.yml -f ${ROOT}/docker-compose.dev.yml"

cleanup() {
  QNAP_PROJECT_ROOT="${ROOT}" \
  QNAP_DATA_ROOT="${TMP_ROOT}" \
    docker compose ${COMPOSE_FILES} down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -f "${ROOT}/.env"
  rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT INT TERM

command -v docker >/dev/null 2>&1 || {
  echo "Docker is required for smoke-test.sh" >&2
  exit 1
}

cp "${ROOT}/.env.example" "${ROOT}/.env"
mkdir -p \
  "${TMP_ROOT}/postgres-data" \
  "${TMP_ROOT}/prometheus-data" \
  "${TMP_ROOT}/grafana-data" \
  "${TMP_ROOT}/host-cache" \
  "${TMP_ROOT}/pcie-cache" \
  "${TMP_ROOT}/dashboard-runtime"

if [ ! -d /share ] || [ ! -d /etc/config ]; then
  if command -v sudo >/dev/null 2>&1; then
    sudo mkdir -p /share /etc/config
  else
    echo "Smoke test requires existing /share and /etc/config bind sources" >&2
    exit 1
  fi
fi

export QNAP_PROJECT_ROOT="${ROOT}"
export QNAP_DATA_ROOT="${TMP_ROOT}"
export NAS_IP=127.0.0.1
export SNMP_COMMUNITY=public
export GF_SECURITY_ADMIN_PASSWORD=smoke_test_only
export POSTGRES_PASSWORD=smoke_test_only
export GF_DATABASE_PASSWORD=smoke_test_only

echo "Building and starting the complete development stack"
docker compose ${COMPOSE_FILES} up -d --build --remove-orphans

wait_url() {
  name="$1"
  url="$2"
  attempts=0
  while [ "${attempts}" -lt 60 ]; do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      echo "Ready: ${name}"
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 2
  done
  echo "Timed out waiting for ${name}: ${url}" >&2
  return 1
}

wait_url qnap-exporter "http://127.0.0.1:${QNAP_EXPORTER_PORT:-39273}/metrics"
wait_url qnap-host-exporter "http://127.0.0.1:${QNAP_HOST_EXPORTER_PORT:-39274}/metrics"
wait_url qnap-pcie-exporter "http://127.0.0.1:${QNAP_PCIE_EXPORTER_PORT:-39275}/metrics"
wait_url qnap-process-exporter "http://127.0.0.1:${QNAP_PROCESS_EXPORTER_PORT:-39276}/metrics"
wait_url prometheus "http://127.0.0.1:${PROMETHEUS_PORT:-39090}/-/ready"
wait_url grafana "http://127.0.0.1:${GRAFANA_PORT:-3300}/api/health"

echo "Checking generated dashboard"
python3 - "${TMP_ROOT}/dashboard-runtime/qnap-cn.json" <<'PY'
import json
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
if not path.is_file():
    raise SystemExit(f"Dashboard builder did not create {path}")
json.loads(path.read_text(encoding="utf-8"))
PY

echo "Checking Prometheus targets"
python3 - "http://127.0.0.1:${PROMETHEUS_PORT:-39090}/api/v1/targets" <<'PY'
import json
import sys
import urllib.request

with urllib.request.urlopen(sys.argv[1], timeout=10) as response:
    payload = json.load(response)
jobs = {
    item.get("labels", {}).get("job")
    for item in payload.get("data", {}).get("activeTargets", [])
}
required = {
    "qnap_exporter",
    "qnap_host_exporter",
    "qnap_pcie_exporter",
    "qnap_process_exporter",
}
missing = required - jobs
if missing:
    raise SystemExit(f"Prometheus targets are missing: {sorted(missing)}")
print("Prometheus jobs:", ", ".join(sorted(required)))
PY

echo "Checking historical fatal errors"
logs="$(docker compose ${COMPOSE_FILES} logs --no-color)"
for pattern in \
  'cannot open /opt/qnap/qnap-detect-platform.sh' \
  'Syntax error: "(" unexpected' \
  'snmptable: executable file not found' \
  'filesystem=1(stat' \
  'volume=1(readlink' \
  'A required service exited'
do
  if printf '%s\n' "${logs}" | grep -F "${pattern}" >/dev/null; then
    echo "Historical fatal error detected: ${pattern}" >&2
    exit 1
  fi
done

docker compose ${COMPOSE_FILES} ps
echo "Smoke test completed successfully"
