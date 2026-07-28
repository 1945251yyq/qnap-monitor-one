# QNAP Monitor One

一个容器完成 QNAP NAS 状态采集、30 天历史存储和中文 Grafana 大屏。

容器内部运行：

- Telegraf：SNMP、宿主机、硬盘、共享文件夹、网络及可选 PCIe/GPU 采集。
- Prometheus：时序历史和 PromQL。
- Grafana：中文监控大屏。

项目不包含 PostgreSQL、独立进程采集服务和动态仪表盘生成服务。相较参考版的
8 个容器，Container Station 中只显示 1 个容器。

## 选择部署方式

### 视频观众：零文件部署

镜像发布后，观众只需复制下面其中一个文件的内容：

- `compose-video.yaml`：普通版，不需要特权模式。
- `compose-video-gpu.yaml`：GPU/PCIe 版，需要 `privileged: true`。

修改 NAS IP、名称、SNMP 社区名和 Grafana 密码，粘贴到
**Container Station → 应用程序 → 创建**。

数据保存在 Docker 命名卷 `qnap_monitor_one_data`。

### 可修改版本

把项目上传到：

```text
/share/Container/qnap-monitor-one
```

修改 `qnap.env`，然后在 Container Station 粘贴：

- `compose.yaml`：普通版。
- `compose-gpu.yaml`：GPU/PCIe 版。

数据保存在 `/share/Container/qnap-monitor-one/data`。

## 访问

```text
http://NAS_IP:3300
```

独立显示屏：

```text
http://NAS_IP:3300/d/qnap-nas-lite/?kiosk
```

GPU 版仪表盘 UID 是 `qnap-nas-lite-gpu`：

```text
http://NAS_IP:3300/d/qnap-nas-lite-gpu/?kiosk
```

也可以直接访问 `/`，Grafana 已配置默认主页。

## GPU/PCIe 说明

- NVIDIA 完整数据依赖 QNAP NVIDIA GPU Driver 套件及可执行的 `nvidia-smi`。
- AMD 等设备优先读取标准 DRM/hwmon。
- 无法自动确认的扩展卡可通过 `QNAP_PCIE_INCLUDE_BDFS` 指定 PCI 地址。
- GPU Compose 使用特权模式。普通 NAS 应使用非 GPU Compose。

## 本地构建

```sh
docker compose -f compose-build.yaml build
docker compose -f compose-build.yaml up -d
```

## 公开镜像

默认从 Docker Hub 拉取：

```text
ydxian/qnap-monitor-one:latest
```

GitHub Actions 会同时构建 `linux/amd64` 和 `linux/arm64`，并推送到：

- Docker Hub：`ydxian/qnap-monitor-one`
- GHCR：`ghcr.io/1945251yyq/qnap-monitor-one`

两个镜像均可匿名拉取，观众不需要登录。正式视频建议固定使用版本标签，
例如 `ydxian/qnap-monitor-one:v1.0.0`，避免未来的 `latest` 更新影响教程。

## 资源与取舍

“单容器”主要减少部署复杂度，不会让 Telegraf、Prometheus 和 Grafana 的实际
工作量消失。默认仍保存 30 天历史，可用 `PROMETHEUS_RETENTION` 调小。

轻量核心版没有高负载进程表。该功能需要持续遍历宿主机 `/proc`、暴露命令行
标签，并容易产生较高时序基数和隐私风险，因此没有纳入默认项目。

## 安全

- 修改默认 Grafana 密码。
- 不要把 Prometheus 和 Telegraf 端口暴露到局域网；镜像只公开 Grafana 3000。
- GPU 版的 `privileged: true` 具有较高宿主机权限。
- `/share`、`/proc`、`/sys` 和 `/etc/config` 均以只读方式挂载。
