# QNAP Status Lite

为威联通 QTS / QuTS hero 重新开发的轻量设备状态面板。它不是把 Grafana、Prometheus、PostgreSQL、Telegraf 等程序打进同一镜像，而是一个约数 MB 的 Go 程序，在一个容器、一个进程内完成采集、历史记录、API 和网页显示。

适合在 Container Station 直接创建项目，也适合后期把浏览器全屏显示在独立屏幕上。

## 它有多轻

| 项目 | QNAP Status Lite |
|---|---|
| 容器数量 | 1 |
| 容器内主要进程 | 1 |
| 数据库 | 内置 bbolt 单文件 |
| 外部前端依赖 | 无 |
| Prometheus / Grafana / PostgreSQL | 不需要 |
| 支持架构 | amd64、arm64 |

## 监控内容

- NAS 型号、主机名、QTS / QuTS hero、运行时间
- CPU、内存、ZFS ARC、CPU 与系统温度
- 风扇、硬盘状态、SMART、温度和容量
- 主存储池总量、共享文件夹占用、可用容量和使用率；QuTS 的内部 ZFS 数据集会自动按池去重
- 每个共享文件夹的名称、所在卷、真实路径、目录容量及占总存储比例
- 物理网卡链路、速率、实时吞吐和启动以来累计收发量
- 主机高占用进程；命令行中的密码、令牌、密钥等参数会自动遮盖
- PCIe 扩展显卡、网卡、NVMe/存储卡和 AI 加速卡
- NVIDIA、AMD、Intel GPU 可从标准 sysfs、DRM、hwmon 与 NVIDIA proc 接口读取可用指标
- 自动识别 QNAP `NVIDIA_GPU_DRV` 套件并读取 GPU、显存、温度、风扇、功耗和编解码负载
- CPU/内存、设备温度、风扇、网络四组独立趋势，每组显示当前、最高和平均值
- 本地 1 小时、6 小时、24 小时和 7 天历史
- JSON API、健康检查和可选的 Prometheus `/metrics` 接口

TS-673A、TS-873A 等 x86 QNAP 可使用 `linux/amd64` 镜像；ARM 机型使用同一标签自动选择 `linux/arm64`。

## Container Station 部署

### 1. 准备目录

在 File Station 中创建：

```text
/share/Container/qnap-status-lite/
├── compose.yaml
├── .env
└── data/
```

下载仓库中的 `compose.yaml` 和 `.env.example`，上传到这个目录，并把 `.env.example` 改名为 `.env`。

### 2. 开启 QNAP SNMP

在 QTS / QuTS hero 控制台中启用 SNMP v2c，设置团体名称。然后编辑 `.env`：

```dotenv
NAS_IP=192.168.1.100
QNAP_HOSTNAME=客厅-NAS
SNMP_COMMUNITY=public
```

`NAS_IP` 填写 NAS 自己的局域网 IP。桥接网络容器中的 `127.0.0.1` 指向容器自身，不能代替 NAS 地址。

### 3. 创建项目

打开 **Container Station → 应用程序 → 创建**，粘贴 `compose.yaml` 内容并部署。完成后访问：

```text
http://NAS地址:3300
```

Compose 里只有一个服务。`/proc`、`/sys`、`/etc/config` 与 `/share` 均为只读挂载，用于读取真实 NAS 状态；只有下面这个目录可写：

```text
/share/Container/qnap-status-lite/data:/data
```

历史记录保存在 `data/history.db`，升级或重建容器不会丢失。

## `.env` 为什么有用

`.env` 不会被镜像“自动读取”。Compose 的这两处配置让它生效：

```yaml
env_file:
  - /share/Container/qnap-status-lite/.env
```

- `env_file` 使用 NAS 上的绝对路径，所以无论从 Container Station 粘贴 Compose，还是从文件创建项目，都能找到同一份配置。
- 它把 NAS 地址、SNMP 团体名称和采集选项传进容器；不需要把这些内容写死在 Compose 中。
- `.env` 已被 Git 忽略，避免把设备地址或 SNMP 团体名称公开到 GitHub。
- 网页端口固定映射为 `3300:8080`；需要改端口时，只编辑 Compose 的左侧数字。

## PCIe 与显卡

程序会优先显示带物理槽位信息的设备、具有独立显存 DRM 接口的 GPU，以及 NVIDIA 驱动登记的设备，从而避免把主板上的每个控制器都列出来。

某些 QNAP 固件不会提供 `physical_slot`。可在 NAS 的 `/sys/bus/pci/devices/` 查看 BDF，并在 `.env` 手动指定：

```dotenv
PCIE_INCLUDE_BDFS=0000:01:00.0
```

显卡能显示多少指标由 QNAP 固件和驱动实际开放的 sysfs/proc 接口决定。即使厂商驱动没有提供利用率与显存数据，设备型号、驱动、PCIe 链路和可读取的温度仍会显示。

程序还会从 `/etc/config/qpkg.conf` 定位 QNAP 官方 `NVIDIA_GPU_DRV`，并尝试调用套件自带的 `nvidia-smi`。如果页面能识别显卡、但没有完整利用率，可在 Compose 的同一个服务中增加：

```yaml
privileged: true
```

然后重新创建项目。这仍然只有一个容器；该选项只是允许容器访问 QNAP 创建的 GPU 设备节点。没有独立显卡时不要开启。

## 主要设置

| 变量 | 默认值 | 说明 |
|---|---:|---|
| `NAS_IP` | 必填 | NAS 的局域网 IP |
| `SNMP_COMMUNITY` | `public` | SNMP v2c 团体名称 |
| `COLLECT_INTERVAL` | `10s` | 实时采集间隔 |
| `HISTORY_RETENTION` | `720h` | 历史保留时间 |
| `PROCESS_TOP_N` | `15` | 显示的进程数量 |
| `SHARE_SIZE_SCAN` | `true` | 是否在后台递归计算共享目录容量 |
| `SHARE_SCAN_INTERVAL` | `1h` | 共享目录容量重新统计间隔 |
| `PCIE_INCLUDE_BDFS` | 空 | 强制显示的 PCIe BDF |
| `PCIE_EXCLUDE_BDFS` | 空 | 隐藏的 PCIe BDF |

共享目录容量由后台任务每小时统计一次，不会阻塞 CPU、温度、网络等实时采集。首次部署时页面会先显示共享文件夹名称和路径，并在统计完成后补上容量；大量小文件的 NAS 如果不希望产生目录遍历 I/O，可以把 `SHARE_SIZE_SCAN` 改为 `false`。

页面主体展示的是用户在 File Station 中看到的共享文件夹。`ZFS18_DATA`、`CACHEDEV1_DATA` 等威联通内部挂载点不会再伪装成多个存储卷重复占据主界面，仅收纳在底部折叠的“内部存储数据集（诊断信息）”中。

## API

- `/api/status`：当前完整状态
- `/api/history?hours=24`：历史趋势
- `/api/health`：容器健康检查
- `/metrics`：Prometheus 文本格式，可供已有监控系统选用

## 开发

```bash
go test ./...
go run ./cmd/qnap-status-lite
```

本项目采用 MIT 许可证。实现为原创代码；参考项目仅用于比较功能边界与部署方式，没有复制无许可证代码或 GPL 项目的实现。
