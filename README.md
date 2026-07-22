# kvmctl

[![CI](https://github.com/Kevin-HYX/kvmctl/actions/workflows/ci.yml/badge.svg)](https://github.com/Kevin-HYX/kvmctl/actions/workflows/ci.yml)
[![最新版本](https://img.shields.io/github/v/release/Kevin-HYX/kvmctl?display_name=tag)](https://github.com/Kevin-HYX/kvmctl/releases/latest)
[![许可证](https://img.shields.io/github/license/Kevin-HYX/kvmctl)](LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/Kevin-HYX/kvmctl)](go.mod)

**把一块低成本 Linux 开发板变成面向 AI 和浏览器的硬件级 KVM。**

`kvmctl` 从被控主机的 HDMI 输出采集画面，并通过 USB Gadget 模拟键盘和鼠标。外部 AI 可以经 SSH 启动 MCP Server，完成人机界面上的“观察、判断、操作”循环；人类也可以临时打开浏览器控制页，直接接管键盘和鼠标。

它不依赖被控主机的操作系统、远程桌面服务或网络连接，因此可以操作 BIOS/UEFI、引导程序、系统安装界面以及失去网络的主机。

> 香橙派 One 是当前唯一完成真机验收的一级支持硬件。其他具备 Linux、视频采集和 USB Device/OTG 能力的板卡属于实验性适配。

## 为什么使用 kvmctl

- **AI 原生控制接口**：内置 MCP Server，工具和错误均为结构化定义，并提供自描述资源帮助 AI 自动理解能力。
- **覆盖操作系统之外的界面**：输入以真实 USB HID 设备呈现，可用于 BIOS/UEFI、启动菜单和系统安装程序。
- **人类随时接管**：通过一次性链接打开浏览器控制页；人类输入优先于 MCP，空闲五秒后自动释放控制权。
- **实时优先的视频链路**：浏览器默认使用 WebRTC 传输 MJPEG，拥塞时丢弃旧画面而不是持续累积延迟；WebSocket 转发模式始终可手动选择。
- **明确的安全边界**：服务只监听回环地址，不向 AI 暴露 Shell、文件传输、配置修改或提权入口。
- **为低成本硬件设计**：核心程序是单文件 Go 二进制，可部署到 ARMv7、ARM64 和 AMD64 Linux。

## 工作原理

```text
                         ┌─────────────────────┐
       MCP / 浏览器      │   Linux 控制设备     │
  AI 或人类 ───────────▶ │      kvmctl         │
                         │                     │
被控主机 HDMI ─▶ 采集卡 ─▶│ V4L2 画面采集       │
                         │                     │
被控主机 USB  ◀───────── │ USB Gadget HID      │
                         └─────────────────────┘
                              键盘 + 双指针
```

画面和输入是两条独立链路：

1. 被控主机的 HDMI 输出经 USB 采集卡进入控制设备的 USB Host 口。
2. 控制设备的 USB OTG/Device 口连接被控主机的 USB Host 口，并模拟键盘、绝对指针和相对指针。
3. AI 通过 MCP 获取当前截图并发送原子输入操作；人类可通过临时浏览器会话观察和控制。

## 功能概览

| 能力 | 说明 |
| --- | --- |
| 屏幕观察 | 直接通过 Linux V4L2 读取 MJPEG 或 YUYV，不依赖 FFmpeg；无信号时明确失败，不返回旧画面 |
| 键盘 | 组合键、有限时长按键和 US 布局 ASCII 文本输入；动作结束后自动释放 |
| 绝对指针 | 使用与分辨率无关的 `0～1000` 归一化坐标，适合桌面和多数图形界面 |
| 相对指针 | 发送 HID 相对移动量，作为 BIOS/UEFI 或特殊固件的兼容模式 |
| MCP | 支持 stdio 和 Streamable HTTP，工具列表根据有效配置动态公布 |
| 浏览器控制 | 固定 `1920×1080`、最高 30 FPS，支持 WebRTC 直连与显式 WebSocket 转发 |
| 控制权仲裁 | 多客户端可同时观察，任意时刻只有一个参与者能够输入；人类浏览器优先 |
| 诊断 | `status`、`doctor` 和设备发现命令提供结构化运行状态与恢复建议 |

## 硬件要求

一级支持组合：

- 香橙派 One；
- 可运行 Armbian 的 microSD 卡与稳定的独立 5V 电源；
- 支持 `1920×1080 @ 30 FPS` MJPEG 或 YUYV 的 UVC HDMI 采集卡；
- 一条 HDMI 线；
- 一条包含数据线芯、支持 USB Device/OTG 的 USB 线；
- 具有 HDMI 输出和 USB Host 接口的被控主机。

不要使用 USB-A 公对公线连接两个 Host 端口。连接器外形相同也不代表线缆包含数据线芯或支持 Device 模式。详细接线、内核要求和真机验收基线见[香橙派 One 指南](docs/hardware/orangepi-one.md)。

## 快速开始

### 1. 下载并安装

以下示例适用于香橙派 One。请先在控制设备上安装 `git`、`curl` 和 `coreutils`：

```sh
git clone https://github.com/Kevin-HYX/kvmctl.git
cd kvmctl

curl -fLO https://github.com/Kevin-HYX/kvmctl/releases/latest/download/kvmctl-linux-armv7
curl -fLO https://github.com/Kevin-HYX/kvmctl/releases/latest/download/SHA256SUMS
sha256sum --ignore-missing -c SHA256SUMS

sudo ./scripts/install.sh ./kvmctl-linux-armv7
```

其他 Linux 架构可分别下载 `kvmctl-linux-arm64` 或 `kvmctl-linux-amd64`。发布页同时提供所有文件的 SHA-256 校验值。

### 2. 配置和检查

```sh
sudoedit /etc/kvmctl/config.toml

sudo kvmctl config validate --file /etc/kvmctl/config.toml
sudo kvmctl devices screen
sudo kvmctl doctor
```

只有一个采集设备时可以保留 `screen.device = ""` 让程序自动发现；存在多个设备时，应使用 `kvmctl devices screen` 返回的 `/dev/v4l/by-id/` 稳定路径。完整字段说明见[配置文档](docs/configuration.md)。

### 3. 启动服务

```sh
sudo systemctl enable --now kvmctl.service
sudo systemctl status kvmctl-gadget.service kvmctl.service
```

`kvmctl-gadget.service` 在开机时以 root 创建 USB Gadget；长期运行的 `kvmctl.service` 使用无登录权限的专用用户，并只通过设备权限访问视频和 HID 节点。

## 让 AI 通过 SSH 使用 MCP

最简单的远程方式是让 MCP 客户端以 SSH 作为 stdio 传输层：

```toml
[mcp_servers.kvmctl]
command = "ssh"
args = ["orangepi", "kvmctl", "mcp", "--transport", "stdio"]
```

其中 `orangepi` 是本机 `~/.ssh/config` 中配置的主机别名。连接后，AI 可以从 `tools/list` 自动发现当前可用操作，也可以读取以下资源：

- `kvmctl://guide`：推荐控制流程与坐标约定；
- `kvmctl://key-names`：键盘工具支持的按键名称；
- `kvmctl://errors`：稳定错误码及恢复建议。

主要 MCP 工具包括：

- `get_status`、`get_screen`；
- `key_press`、`type_text`；
- 绝对移动、点击与拖拽；
- 相对移动、点击、拖拽与滚轮；
- `release_all`、`release_control`；
- 浏览器会话的创建、状态查询和关闭。

完整参数和错误定义见 [MCP 操作面](docs/spec/operations-v0.1.md)。

## 本机 CLI

CLI 与 MCP 使用相同的控制核心，适合安装验收和故障排查：

```sh
# 检查运行状态和硬件
kvmctl status
kvmctl doctor
kvmctl devices screen

# 截取带坐标标注的当前画面
kvmctl screen --view coordinates --format jpeg --output screen.jpg

# 发送键盘和鼠标操作
kvmctl key press ctrl alt delete
kvmctl mouse click-absolute --x 500 --y 500 --button left

# 紧急释放全部按键和鼠标按钮
kvmctl release-all
```

## 浏览器控制

统一服务在回环地址同时提供 MCP 与浏览器控制端点：

```sh
kvmctl serve --listen 127.0.0.1:8080
```

AI 调用 `browser_session_open` 后会得到一个十分钟内可兑换一次的临时入口。页面连接后不设置固定会话时长；断线有六十秒重连窗口，最后一次输入五秒后立即释放控制权。

浏览器默认尝试 WebRTC 直连，并通过独立 DataChannel 传输画面和可靠控制消息。若网络环境无法直连，操作者可以在页面中显式切换到 WebSocket 转发模式；服务不会在后台静默切换传输路径。协议细节见[浏览器传输说明](docs/spec/browser-transports-v0.2.md)。

## 安全说明

拥有 `kvmctl` 控制权限，等同于拥有被控主机的本地屏幕、键盘和鼠标访问权限。

- MCP HTTP 和统一服务只允许监听回环地址，不能直接暴露到局域网或公网。
- 远程访问应由可信边缘代理终止 HTTPS、验证身份、限制请求频率，再转发到控制设备。
- 浏览器入口使用 256 位随机令牌，只能兑换一次；入口 URL 的查询参数不得写入访问日志。
- 截图默认只在内存中处理，CLI 只有在显式指定输出路径时才写入磁盘。
- 日志不记录截图、文本输入内容、具体按键、坐标或完整工具参数。
- MCP 不提供任意 Shell、文件传输、配置写入、软件升级或提权工具。

漏洞请按照[安全策略](SECURITY.md)私下报告，不要在公开 Issue 中披露。

## 已知边界

- 当前不支持电源、复位或 ATX GPIO 控制。
- USB HID 发送物理按键，不是 Unicode；`type_text` 只支持 US 键盘布局下可确定输入的可打印 ASCII。
- 浏览器视频要求采集设备提供 `1920×1080 @ 30 FPS` 的 MJPEG 或 YUYV，不会静默降级到较低分辨率。
- 首版 WebRTC 支持 STUN，不内置 TURN；复杂 NAT 环境可能需要使用 WebSocket 转发。
- 绝对指针取决于被控固件对 USB HID Absolute Pointer 的支持；不兼容时应切换为相对指针。
- 除香橙派 One 外的 Linux 板卡尚未纳入一级硬件回归。

## 文档

- [配置说明](docs/configuration.md)
- [香橙派 One 接线与验收](docs/hardware/orangepi-one.md)
- [MCP 操作面](docs/spec/operations-v0.1.md)
- [浏览器传输说明](docs/spec/browser-transports-v0.2.md)
- [架构决策记录](docs/adr/)
- [贡献指南](CONTRIBUTING.md)
- [获取帮助](SUPPORT.md)
- [安全策略](SECURITY.md)

## 构建与测试

```sh
go test ./...
go vet ./...
go build ./cmd/kvmctl
```

CI 还会执行竞态检测、浏览器传输测试、Gadget 脚本测试，以及 Linux ARMv7、ARM64 和 AMD64 交叉构建。以 `v` 开头的标签会生成三种架构的发布文件与 `SHA256SUMS`。

## 参与贡献

欢迎提交可复现的缺陷、硬件兼容性记录和改进建议。硬件相关报告请注明板卡型号、系统镜像、内核版本、设备树、USB 控制器模式、采集卡能力和实际接线方式。开始实现前请先阅读[贡献指南](CONTRIBUTING.md)。

## 许可证

本项目采用 [Apache License 2.0](LICENSE)。
