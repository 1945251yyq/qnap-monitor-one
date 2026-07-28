# QNAP Monitor One

面向威联通 QTS / QuTS hero 的轻量级 NAS 状态大屏。一个 Docker 容器完成
数据采集、30 天历史存储和中文 Grafana 展示，适合在 Container Station 中部署，
也适合连接独立屏幕长期显示。

```text
Docker Hub：ydxian/qnap-monitor-one:latest
访问地址：http://NAS_IP:3300
```

## 为什么是 QNAP 专用版

这不是只修改了名称的通用监控模板。容器会针对威联通环境进行特征检测：

- 自动区分 QTS 与 QuTS hero。
- 识别 `CACHEDEV*_DATA`、`ZFS*_DATA` 和共享文件夹真实路径。
- 读取威联通 `/etc/config/smb.conf`，避免仅按目录名称猜测共享文件夹。
- 兼容 QuTS hero 的 ZFS ARC 路径。
- 采集 CPU、内存、网络、存储池、硬盘和共享文件夹状态。
- GPU 版可识别 PCIe 插槽、显卡、网卡、NVMe 及部分硬件传感器。
- 对 TS-673A、TS-873A 等可扩展机型提供可选的 PCIe/GPU 采集模式。

容器内部整合 Telegraf、Prometheus 和 Grafana。相比多容器参考方案，
Container Station 中只需要管理一个容器，不再额外部署 PostgreSQL、
独立 Exporter 或动态面板服务。

## 部署前准备

1. 在 QNAP 控制台启用 SNMP 服务。
2. 确认 SNMP 社区名，常见默认值是 `public`。
3. 确认 NAS 的局域网 IP。
4. Container Station 必须能够访问 Docker Hub。
5. 建议提前建立目录：

```text
/share/Container/qnap-monitor-one/data
```

Container Station 通常也可以自动创建该目录。

## 选择部署方式

| 使用场景 | Compose 文件 | `qnap.env` | 权限 |
| --- | --- | --- | --- |
| 普通 NAS，直接粘贴 | [`compose-video.yaml`](compose-video.yaml) | 不需要 | 非特权 |
| GPU/PCIe，直接粘贴 | [`compose-video-gpu.yaml`](compose-video-gpu.yaml) | 不需要 | 特权 |
| 独立配置文件 | [`compose.yaml`](compose.yaml) | 必须上传 | 非特权 |
| 独立配置文件、GPU/PCIe | [`compose-gpu.yaml`](compose-gpu.yaml) | 必须上传 | 特权 |

不确定时使用 `compose-video.yaml`。

## 方法一：直接粘贴，推荐视频观众使用

打开 [`compose-video.yaml`](compose-video.yaml)，只修改下面四项：

```yaml
NAS_IP: "192.168.1.100"
QNAP_HOSTNAME: "QNAP-NAS"
SNMP_COMMUNITY: "public"
GF_SECURITY_ADMIN_PASSWORD: "请改成强密码"
```

然后复制完整内容，进入：

```text
Container Station → 应用程序 → 创建
```

粘贴 YAML、验证并创建应用。该版本的环境变量已经写在 Compose 中，
不会读取 GitHub 里的 `qnap.env`。

需要显卡或 PCIe 数据时改用
[`compose-video-gpu.yaml`](compose-video-gpu.yaml)。GPU 版包含
`privileged: true`，普通 NAS 不应使用。

## 方法二：使用独立 `qnap.env`

适合希望把密码、NAS 地址和可选参数集中管理的用户。

将以下文件上传到：

```text
/share/Container/qnap-monitor-one/
├── qnap.env
└── data/
```

编辑 [`qnap.env`](qnap.env)，再把 [`compose.yaml`](compose.yaml) 或
[`compose-gpu.yaml`](compose-gpu.yaml) 粘贴到 Container Station。

这两个 Compose 会通过绝对路径读取：

```yaml
env_file:
  - /share/Container/qnap-monitor-one/qnap.env
```

`qnap.env` 只对这两个配置文件版 Compose 和本地构建有效；
两个 `compose-video*.yaml` 不会读取它。

## QNAP 挂载说明

以下挂载不是普通模板遗留，而是读取威联通宿主机状态所必需：

| 挂载 | 权限 | 用途 |
| --- | --- | --- |
| `/share:/share` | 只读 | QTS/QuTS 存储卷、共享文件夹和容量 |
| `/proc:/host-proc` | 只读 | 宿主机、ZFS ARC 和部分 NVIDIA 状态 |
| `/sys:/host-sys` | 只读 | PCIe、GPU、网卡、NVMe、DRM/hwmon |
| `/etc/config:/host-etc-config` | 只读 | 威联通 `smb.conf` 和共享文件夹映射 |
| `/share/Container/qnap-monitor-one/data:/data` | 读写 | Grafana、Prometheus 历史和运行缓存 |

前四项全部带有 `:ro`，容器不能通过这些挂载修改 NAS 文件。
最后一项使用 QNAP 共享目录而不是 Docker 命名卷，便于通过 File Station
查看、备份和迁移。

## 访问监控大屏

部署完成后访问：

```text
http://NAS_IP:3300
```

默认用户名是 `admin`，密码为 Compose 或 `qnap.env` 中设置的
`GF_SECURITY_ADMIN_PASSWORD`。

普通版独立屏幕地址：

```text
http://NAS_IP:3300/d/qnap-nas-lite/?kiosk
```

GPU/PCIe 版独立屏幕地址：

```text
http://NAS_IP:3300/d/qnap-nas-lite-gpu/?kiosk
```

浏览器使用全屏或 kiosk 模式即可作为 NAS 状态屏长期显示。

## 常用环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `NAS_IP` | `192.168.1.100` | QNAP NAS 地址，必须修改 |
| `QNAP_HOSTNAME` | `QNAP-NAS` | 面板中显示的设备名称 |
| `SNMP_COMMUNITY` | `public` | 必须与 QNAP SNMP 设置一致 |
| `GF_SECURITY_ADMIN_USER` | `admin` | Grafana 管理员账号 |
| `GF_SECURITY_ADMIN_PASSWORD` | 示例占位符 | 必须修改 |
| `TZ` | `Asia/Shanghai` | 容器时区 |
| `PROMETHEUS_RETENTION` | `30d` | 历史数据保留时间 |
| `QNAP_SHARE_SCAN_INTERVAL` | `3600` | 共享文件夹重新扫描间隔，单位秒 |
| `QNAP_DATA_VOLUME_NAME` | 空 | 可选，手动指定 `ZFS*_DATA` 或 `CACHEDEV*_DATA` |
| `QNAP_GPU_ENABLED` | `false` | GPU Compose 会覆盖为 `true` |
| `QNAP_PCIE_INCLUDE_BDFS` | 空 | 强制包含指定 PCI 地址，逗号分隔 |
| `QNAP_PCIE_EXCLUDE_BDFS` | 空 | 排除指定 PCI 地址，逗号分隔 |

## GPU/PCIe 说明

- NVIDIA 完整指标依赖 QNAP NVIDIA GPU Driver 套件和可执行的 `nvidia-smi`。
- AMD 等设备优先读取标准 DRM/hwmon 接口，实际指标取决于驱动暴露能力。
- PCIe 采集采用设备类别、物理插槽和驱动特征过滤，减少把主板内部设备误认为扩展卡。
- 无法自动确认的设备可通过 `QNAP_PCIE_INCLUDE_BDFS` 指定，例如
  `0000:01:00.0`。
- GPU Compose 使用 `privileged: true`。这是访问部分 QNAP GPU/PCIe
  信息所需的高权限模式，请只在确有需求时启用。

## 数据、备份与升级

所有持久数据位于：

```text
/share/Container/qnap-monitor-one/data
```

备份时建议先停止应用，再复制整个 `data` 目录。重新创建或更新容器不会删除
该目录中的 Grafana 与 Prometheus 数据。

升级镜像：

1. 在 Container Station 中停止应用。
2. 拉取新版 `ydxian/qnap-monitor-one:latest`。
3. 重新创建或更新应用。
4. 确认 `/share/Container/qnap-monitor-one/data:/data` 挂载没有改变。

正式视频教程建议固定版本标签，例如
`ydxian/qnap-monitor-one:v1.0.0`，避免未来的 `latest` 更新影响教程。

## 常见问题

### 面板没有数据

- 确认 QNAP SNMP 已启用。
- 确认 `NAS_IP` 是 NAS 地址，不是观看设备地址。
- 确认 `SNMP_COMMUNITY` 与 QNAP 设置完全一致。
- 确认 NAS 防火墙没有拦截容器访问 UDP 161。

### 共享文件夹或容量为空

- 确认 `/share` 和 `/etc/config` 按 Compose 只读挂载。
- 确认数据卷实际名称符合 QNAP 的 `CACHEDEV*_DATA` 或 `ZFS*_DATA`。
- 多存储卷环境可设置 `QNAP_DATA_VOLUME_NAME`。

### GPU 页面没有完整数据

- 确认使用的是 GPU Compose。
- 确认 QNAP 已安装适配当前显卡的驱动。
- 确认容器处于特权模式。
- NVIDIA 用户应确认 QNAP 主机上的 `nvidia-smi` 能正常运行。

### 重建容器后历史消失

确认使用的是：

```yaml
- /share/Container/qnap-monitor-one/data:/data
```

不要删除宿主机上的 `data` 目录。

## 镜像与架构

公开镜像同时构建 `linux/amd64` 和 `linux/arm64`：

- Docker Hub：`ydxian/qnap-monitor-one`
- GHCR：`ghcr.io/1945251yyq/qnap-monitor-one`

两个仓库均支持匿名拉取。Docker Hub 示例：

```sh
docker pull ydxian/qnap-monitor-one:latest
```

## 本地构建

本地测试使用 [`compose-build.yaml`](compose-build.yaml)，它读取项目目录中的
`qnap.env`，并将数据写入本地 `./data`：

```sh
docker compose -f compose-build.yaml build
docker compose -f compose-build.yaml up -d
```

## 资源与安全取舍

“单容器”减少的是部署与维护复杂度，不会消除 Telegraf、Prometheus 和 Grafana
本身的资源需求。默认保留 30 天历史，可通过 `PROMETHEUS_RETENTION` 调小。

轻量核心版没有高负载进程表。持续遍历宿主机 `/proc` 并将命令行作为标签，
容易产生高时序基数和隐私风险，因此没有纳入默认功能。

安全建议：

- 必须修改 Grafana 默认密码。
- 不要把 UDP 161、Prometheus 或 Telegraf 端口暴露到互联网。
- 镜像只对外发布 Grafana 的 3000 端口，Compose 映射为 NAS 的 3300。
- 普通 NAS 优先使用非特权版。
- 定期备份 `/share/Container/qnap-monitor-one/data`。

第三方组件及许可证信息见
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md)。
