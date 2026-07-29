#!/usr/bin/env python3
import copy
import json
import os
import time
import urllib.request

TEMPLATE = os.environ.get("QNAP_DASHBOARD_TEMPLATE", "/templates/qnap-cn-base.json")
OUTPUT = os.environ.get("QNAP_DASHBOARD_OUTPUT", "/runtime/qnap-cn.json")
STATE = os.environ.get("QNAP_DASHBOARD_STATE", "/runtime/.pcie-layout-state.json")
METRICS_URL = os.environ.get("QNAP_PCIE_METRICS_URL", "http://qnap-pcie-exporter:9275/metrics")
INTERVAL = max(30, int(os.environ.get("QNAP_DASHBOARD_REFRESH_SECONDS", "60")))
PCIE_IDS = {50, 51, 52, 53, 54, 55}
PROCESS_IDS = {60, 61}


def read_template():
    with open(TEMPLATE, "r", encoding="utf-8") as handle:
        return json.load(handle)


def fetch_metrics():
    request = urllib.request.Request(METRICS_URL, headers={"User-Agent": "qnap-dashboard-builder/1.0"})
    with urllib.request.urlopen(request, timeout=10) as response:
        return response.read().decode("utf-8", errors="replace")


def positive_metric(text, names):
    for raw in text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        metric = line.split("{", 1)[0].split(None, 1)[0]
        if metric not in names:
            continue
        if 'is_expansion="1"' not in line:
            continue
        try:
            value = float(line.rsplit(None, 1)[1])
        except (ValueError, IndexError):
            continue
        if value > 0:
            return True
    return False


def any_metric(text, names):
    for raw in text.splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        metric = line.split("{", 1)[0].split(None, 1)[0]
        if metric in names and 'is_expansion="1"' in line:
            return True
    return False


def detect(text):
    devices = positive_metric(text, {"qnap_pcie_device_snapshot_present"})
    gpu = positive_metric(text, {"qnap_pcie_gpu_present"})
    nic = any_metric(text, {
        "qnap_pcie_nic_receive_bytes_total", "qnap_pcie_nic_transmit_bytes_total",
        "qnap_pcie_nic_link_up"
    })
    storage = positive_metric(text, {"qnap_pcie_storage_present"}) or any_metric(text, {
        "qnap_pcie_storage_read_bytes_total", "qnap_pcie_storage_write_bytes_total"
    })
    sensors = any_metric(text, {
        "qnap_pcie_gpu_temperature_celsius", "qnap_pcie_gpu_fan_percent",
        "qnap_pcie_gpu_power_watts", "qnap_pcie_sensor_temperature_celsius",
        "qnap_pcie_sensor_fan_rpm", "qnap_pcie_sensor_power_watts"
    })
    return {
        "devices": devices,
        "gpu": gpu,
        "sensors": sensors,
        "nic": nic,
        "storage": storage,
    }


def build_dashboard(template, flags):
    dashboard = copy.deepcopy(template)
    pcie_templates = {
        panel.get("id"): copy.deepcopy(panel)
        for panel in dashboard.get("panels", [])
        if panel.get("id") in PCIE_IDS
    }
    process_templates = {
        panel.get("id"): copy.deepcopy(panel)
        for panel in dashboard.get("panels", [])
        if panel.get("id") in PROCESS_IDS
    }
    retained = [
        panel for panel in dashboard.get("panels", [])
        if panel.get("id") not in PCIE_IDS and panel.get("id") not in PROCESS_IDS
    ]
    dashboard["panels"] = retained

    y = max(
        (p.get("gridPos", {}).get("y", 0) + p.get("gridPos", {}).get("h", 0) for p in retained),
        default=0,
    )

    if flags["devices"]:
        row = pcie_templates[50]
        row["gridPos"] = {"x": 0, "y": y, "w": 24, "h": 1}
        table = pcie_templates[51]
        table["gridPos"] = {"x": 0, "y": y + 1, "w": 24, "h": 8}
        dashboard["panels"].extend([row, table])

        detail_ids = []
        if flags["gpu"]:
            detail_ids.append(52)
        if flags["sensors"]:
            detail_ids.append(53)
        if flags["nic"]:
            detail_ids.append(54)
        if flags["storage"]:
            detail_ids.append(55)

        y += 9
        for index in range(0, len(detail_ids), 2):
            pair = detail_ids[index:index + 2]
            if len(pair) == 1:
                panel = pcie_templates[pair[0]]
                panel["gridPos"] = {"x": 0, "y": y, "w": 24, "h": 8}
                dashboard["panels"].append(panel)
            else:
                left = pcie_templates[pair[0]]
                right = pcie_templates[pair[1]]
                left["gridPos"] = {"x": 0, "y": y, "w": 12, "h": 8}
                right["gridPos"] = {"x": 12, "y": y, "w": 12, "h": 8}
                dashboard["panels"].extend([left, right])
            y += 8

    # The current high-load process table is always the final section,
    # regardless of which PCIe capability panels are dynamically visible.
    process_row = process_templates[60]
    process_row["gridPos"] = {"x": 0, "y": y, "w": 24, "h": 1}
    process_table = process_templates[61]
    process_table["gridPos"] = {"x": 0, "y": y + 1, "w": 24, "h": 12}
    dashboard["panels"].extend([process_row, process_table])

    dashboard["version"] = int(time.time())
    return dashboard


def load_state():
    try:
        with open(STATE, "r", encoding="utf-8") as handle:
            return json.load(handle)
    except Exception:
        return None


def write_atomic(path, payload):
    directory = os.path.dirname(path)
    os.makedirs(directory, exist_ok=True)
    temporary = path + ".tmp"
    with open(temporary, "w", encoding="utf-8") as handle:
        json.dump(payload, handle, ensure_ascii=False, indent=2)
        handle.flush()
        os.fsync(handle.fileno())
    os.replace(temporary, path)


def apply(flags):
    signature = {key: bool(value) for key, value in flags.items()}
    if os.path.isfile(OUTPUT) and load_state() == signature:
        return False
    dashboard = build_dashboard(read_template(), signature)
    # Validate the generated document before replacing Grafana's active file.
    json.loads(json.dumps(dashboard, ensure_ascii=False))
    write_atomic(OUTPUT, dashboard)
    write_atomic(STATE, signature)
    visible = [name for name, enabled in signature.items() if enabled]
    print("Dashboard updated; visible PCIe capabilities: " + (", ".join(visible) if visible else "none"), flush=True)
    return True


def main():
    first_run = True
    while True:
        try:
            metrics = fetch_metrics()
            flags = detect(metrics)
            apply(flags)
        except Exception as exc:
            # On the first start, create a valid dashboard without an empty PCIe
            # section so Grafana can start. Later failures preserve the last good file.
            if first_run and not os.path.isfile(OUTPUT):
                apply({"devices": False, "gpu": False, "sensors": False, "nic": False, "storage": False})
            print("PCIe dashboard refresh deferred: " + str(exc), flush=True)
        first_run = False
        time.sleep(INTERVAL)


if __name__ == "__main__":
    main()
