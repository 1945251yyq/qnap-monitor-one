#!/usr/bin/env python3
import os
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PROC_ROOT = os.environ.get("QNAP_PROCESS_PROC_ROOT", "/host-proc")
PASSWD_CANDIDATES = [
    os.environ.get("QNAP_PROCESS_PASSWD", "/host-etc-config/passwd"),
    os.path.join(PROC_ROOT, "1/root/etc/passwd"),
]
HOSTNAME = os.environ.get("QNAP_HOSTNAME", "QNAP-NAS")
# QNAP_PROCESS_EXPORTER_PORT is the host-side Compose mapping configured in .env.
# Keep the container listener independent so changing the published port never
# makes the service unreachable from its health check or Prometheus.
PORT = int(os.environ.get("QNAP_PROCESS_LISTEN_PORT", "9276"))
INTERVAL = max(5, int(os.environ.get("QNAP_PROCESS_SAMPLE_INTERVAL", "15")))
TOP_N = min(50, max(5, int(os.environ.get("QNAP_PROCESS_TOP_N", "15"))))
COMMAND_LIMIT = min(500, max(80, int(os.environ.get("QNAP_PROCESS_COMMAND_LIMIT", "220"))))

STATE_NAMES = {
    "R": "运行",
    "S": "睡眠",
    "D": "不可中断",
    "Z": "僵尸",
    "T": "已停止",
    "t": "跟踪停止",
    "X": "已结束",
    "x": "已结束",
    "I": "内核空闲",
    "P": "已挂起",
}

SENSITIVE_RE = re.compile(
    r"(?i)(password|passwd|secret|token|api[_-]?key|access[_-]?key|auth|credential|community)"
    r"(\s*=\s*|\s+)([^\s]+)"
)

metrics_lock = threading.Lock()
metrics_payload = "# HELP qnap_process_collector_up Process collector status.\n# TYPE qnap_process_collector_up gauge\nqnap_process_collector_up 0\n"


def read_text(path, binary=False):
    mode = "rb" if binary else "r"
    try:
        with open(path, mode, encoding=None if binary else "utf-8", errors=None if binary else "replace") as handle:
            return handle.read()
    except (OSError, IOError):
        return b"" if binary else ""


def load_users():
    result = {}
    for path in PASSWD_CANDIDATES:
        if not path:
            continue
        text = read_text(path)
        if not text:
            continue
        for line in text.splitlines():
            parts = line.split(":")
            if len(parts) < 4:
                continue
            try:
                result[int(parts[2])] = parts[0]
            except ValueError:
                continue
        if result:
            break
    return result


def total_cpu_jiffies():
    line = read_text(os.path.join(PROC_ROOT, "stat")).splitlines()
    if not line:
        raise RuntimeError("host /proc/stat is unavailable")
    fields = line[0].split()
    if not fields or fields[0] != "cpu":
        raise RuntimeError("host /proc/stat has an unexpected format")
    # Exclude guest and guest_nice because they are already included in user/nice.
    return sum(int(value) for value in fields[1:9])


def cpu_count():
    count = 0
    for line in read_text(os.path.join(PROC_ROOT, "stat")).splitlines():
        if re.match(r"^cpu[0-9]+\s", line):
            count += 1
    return max(1, count)


def memory_total_bytes():
    for line in read_text(os.path.join(PROC_ROOT, "meminfo")).splitlines():
        if line.startswith("MemTotal:"):
            parts = line.split()
            if len(parts) >= 2:
                return int(parts[1]) * 1024
    return 0


def parse_status(pid):
    uid = 0
    rss_bytes = 0
    for line in read_text(os.path.join(PROC_ROOT, pid, "status")).splitlines():
        if line.startswith("Uid:"):
            parts = line.split()
            if len(parts) >= 2:
                try:
                    uid = int(parts[1])
                except ValueError:
                    pass
        elif line.startswith("VmRSS:"):
            parts = line.split()
            if len(parts) >= 2:
                try:
                    rss_bytes = int(parts[1]) * 1024
                except ValueError:
                    pass
    return uid, rss_bytes


def clean_command(raw, process_name):
    if raw:
        command = raw.replace(b"\x00", b" ").decode("utf-8", errors="replace").strip()
    else:
        command = "[" + process_name + "]"
    command = " ".join(command.split())
    command = SENSITIVE_RE.sub(lambda match: match.group(1) + match.group(2) + "***", command)
    if len(command) > COMMAND_LIMIT:
        command = command[: COMMAND_LIMIT - 1] + "…"
    return command


def read_processes(users):
    processes = {}
    try:
        entries = os.listdir(PROC_ROOT)
    except OSError as exc:
        raise RuntimeError("cannot list host /proc: " + str(exc))

    for pid in entries:
        if not pid.isdigit():
            continue
        stat_line = read_text(os.path.join(PROC_ROOT, pid, "stat")).strip()
        if not stat_line:
            continue
        left = stat_line.find("(")
        right = stat_line.rfind(")")
        if left < 0 or right <= left:
            continue
        process_name = stat_line[left + 1:right]
        rest = stat_line[right + 2:].split()
        # state through rss must be present.
        if len(rest) < 22:
            continue
        try:
            state = rest[0]
            process_ticks = int(rest[11]) + int(rest[12])
            threads = int(rest[17])
            start_ticks = int(rest[19])
        except (ValueError, IndexError):
            continue
        uid, rss_bytes = parse_status(pid)
        raw_cmdline = read_text(os.path.join(PROC_ROOT, pid, "cmdline"), binary=True)
        processes[pid] = {
            "pid": pid,
            "name": process_name,
            "state": STATE_NAMES.get(state, state),
            "ticks": process_ticks,
            "threads": max(0, threads),
            "start": start_ticks,
            "uid": uid,
            "user": users.get(uid, str(uid)),
            "rss": max(0, rss_bytes),
            "command": clean_command(raw_cmdline, process_name),
        }
    return processes


def read_raw_snapshot():
    users = load_users()
    return {
        "total": total_cpu_jiffies(),
        "cpus": cpu_count(),
        "memory": memory_total_bytes(),
        "processes": read_processes(users),
        "time": time.time(),
    }


def prom_escape(value):
    return str(value).replace("\\", "\\\\").replace("\n", "\\n").replace('"', '\\"')


def labels_for(item):
    labels = {
        "host": HOSTNAME,
        "pid": item["pid"],
        "user": item["user"],
        "process": item["name"],
        "command": item["command"],
        "state": item["state"],
    }
    return "{" + ",".join(f'{key}="{prom_escape(value)}"' for key, value in labels.items()) + "}"


def build_metrics(previous, current):
    delta_total = current["total"] - previous["total"]
    if delta_total <= 0:
        raise RuntimeError("host CPU counters did not advance")
    cpus = current["cpus"]
    memory_total = current["memory"]
    rows = []
    for pid, item in current["processes"].items():
        old = previous["processes"].get(pid)
        if not old or old["start"] != item["start"]:
            cpu_percent = 0.0
        else:
            delta_process = max(0, item["ticks"] - old["ticks"])
            cpu_percent = (delta_process / delta_total) * cpus * 100.0
        cpu_percent = min(cpu_percent, cpus * 100.0)
        memory_percent = (item["rss"] / memory_total * 100.0) if memory_total > 0 else 0.0
        row = dict(item)
        row["cpu_percent"] = max(0.0, cpu_percent)
        row["memory_percent"] = max(0.0, memory_percent)
        rows.append(row)

    # CPU is the primary load criterion; RSS breaks ties so the table remains useful when the NAS is idle.
    rows.sort(key=lambda row: (row["cpu_percent"], row["rss"]), reverse=True)
    rows = rows[:TOP_N]

    lines = [
        "# HELP qnap_process_collector_up Process collector status.",
        "# TYPE qnap_process_collector_up gauge",
        f'qnap_process_collector_up{{host="{prom_escape(HOSTNAME)}"}} 1',
        "# HELP qnap_process_visible_count Number of processes currently exposed in the high-load table.",
        "# TYPE qnap_process_visible_count gauge",
        f'qnap_process_visible_count{{host="{prom_escape(HOSTNAME)}"}} {len(rows)}',
        "# HELP qnap_process_total_seen Number of host processes seen during this sample.",
        "# TYPE qnap_process_total_seen gauge",
        f'qnap_process_total_seen{{host="{prom_escape(HOSTNAME)}"}} {len(current["processes"])}',
        "# HELP qnap_process_cpu_percent Process CPU usage, normalized like top so one fully used CPU equals 100 percent.",
        "# TYPE qnap_process_cpu_percent gauge",
        "# HELP qnap_process_memory_percent Process resident memory as a percentage of total NAS memory.",
        "# TYPE qnap_process_memory_percent gauge",
        "# HELP qnap_process_rss_bytes Process resident memory in bytes.",
        "# TYPE qnap_process_rss_bytes gauge",
        "# HELP qnap_process_thread_count Process thread count.",
        "# TYPE qnap_process_thread_count gauge",
    ]
    for row in rows:
        labels = labels_for(row)
        lines.append(f'qnap_process_cpu_percent{labels} {row["cpu_percent"]:.4f}')
        lines.append(f'qnap_process_memory_percent{labels} {row["memory_percent"]:.4f}')
        lines.append(f'qnap_process_rss_bytes{labels} {row["rss"]}')
        lines.append(f'qnap_process_thread_count{labels} {row["threads"]}')
    return "\n".join(lines) + "\n"


def sampler():
    global metrics_payload
    try:
        previous = read_raw_snapshot()
    except Exception as exc:
        previous = None
        with metrics_lock:
            metrics_payload = (
                "# HELP qnap_process_collector_up Process collector status.\n"
                "# TYPE qnap_process_collector_up gauge\n"
                f'qnap_process_collector_up{{host="{prom_escape(HOSTNAME)}"}} 0\n'
                f'# collector error: {str(exc).replace(chr(10), " ")}\n'
            )

    first_wait = min(3, INTERVAL)
    time.sleep(first_wait)
    while True:
        try:
            current = read_raw_snapshot()
            if previous is not None:
                payload = build_metrics(previous, current)
                with metrics_lock:
                    metrics_payload = payload
            previous = current
        except Exception as exc:
            with metrics_lock:
                metrics_payload = (
                    "# HELP qnap_process_collector_up Process collector status.\n"
                    "# TYPE qnap_process_collector_up gauge\n"
                    f'qnap_process_collector_up{{host="{prom_escape(HOSTNAME)}"}} 0\n'
                    f'# collector error: {str(exc).replace(chr(10), " ")}\n'
                )
        time.sleep(INTERVAL)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path not in ("/metrics", "/metrics/"):
            self.send_response(404)
            self.end_headers()
            return
        with metrics_lock:
            payload = metrics_payload.encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, format_string, *args):
        return


def main():
    thread = threading.Thread(target=sampler, name="qnap-process-sampler", daemon=True)
    thread.start()
    print(
        f"QNAP process exporter listening on :{PORT}; proc={PROC_ROOT}; interval={INTERVAL}s; top={TOP_N}",
        flush=True,
    )
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()


if __name__ == "__main__":
    main()
