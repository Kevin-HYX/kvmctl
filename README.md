# kvmctl

`kvmctl` 是一个面向外部 AI 的开源 KVM 控制服务。它在低成本 Linux 控制设备上采集被控主机的屏幕画面，并把自身模拟成 USB 键盘和鼠标，让 AI 能通过 SSH 或 MCP 完成“观察—决策—操作”的控制循环。

> 当前仓库已包含可运行的 CLI、MCP Server、V4L2 采集和 USB Gadget HID 实现。香橙派 One 的真实硬件验收尚未完成，因此暂未发布稳定版本。

## 目标

- 优先适配香橙派及其 Linux 生态。
- 通过 HDMI 采集设备获得被控主机画面。
- 通过 Linux USB Gadget HID 向被控主机发送键盘、鼠标输入。
- 同一个 `kvmctl` 程序同时提供人类可调试的 CLI 和 AI 可调用的 MCP stdio Server。
- 让采集与输入硬件保持可替换，以支持虚拟测试环境和其他 Linux 板卡。
- 在 32 位 ARM、512 MB 内存等级的设备上保持可部署性。

## 硬件支持等级

- **一级支持：香橙派 One。** 项目提供完整安装、接线和配置说明，相关发布必须经过真实板卡验收。
- **实验性：其他 Linux 板卡。** 具备可用画面来源和 USB Device/OTG 控制器的设备可以尝试运行，但首版不承诺硬件验证或兼容性。
- **虚拟测试：x86_64 Linux 虚拟机。** 仅验证 CLI、MCP、坐标视图和虚拟输入，不代表真实 USB OTG 或 HDMI 采集已经通过。

## 非目标

- 不在控制设备上运行 AI 模型。
- 不在设备端解释“安装系统”等高层任务。
- 不提供任意 Shell 命令执行工具。
- 不把远程桌面协议伪装成硬件级 KVM；目标是能够观察 BIOS、引导程序和操作系统安装界面。

## 使用方式

```text
外部 AI
  │  MCP
  ▼
kvmctl mcp（控制设备）
  ├─ HDMI 采集 → 屏幕观察
  └─ USB Gadget HID → 键盘/鼠标动作
                         │
                         ▼
                      被控主机
```

```shell
# 验证配置和硬件
kvmctl config validate --file /etc/kvmctl/config.toml
kvmctl status
kvmctl doctor
kvmctl devices screen

# 人类调试：只有显式 --output 才会写截图
kvmctl screen --view coordinates --format jpeg --output screen.jpg
kvmctl key press ctrl alt delete
kvmctl mouse click-absolute --x 500 --y 500 --button left

# MCP stdio Server
kvmctl mcp --transport stdio

# MCP Streamable HTTP，只允许回环地址
kvmctl mcp --transport http --listen 127.0.0.1:8080
```

MCP 提供 `get_status`、`get_screen`、`key_press`、`type_text`、绝对与相对鼠标操作、滚轮、`release_all` 和 `release_control`。工具列表按照有效配置动态公布；瞬时硬件不可用通过状态和稳定错误码表达。AI 还可以读取 `kvmctl://guide`、`kvmctl://key-names`、`kvmctl://errors` 三个自描述资源。

绝对指针统一使用左上角 `(0,0)`、右下角 `(1000,1000)` 的固定坐标，不要求操作携带观察对象或预先调用 `get_screen`。相对指针发送有符号 HID 移动量，用于固件兼容兜底，不承诺与屏幕像素一一对应。

`type_text` 只接受按 US 键盘布局可确定输入的 ASCII 字符；USB HID 发送的是物理按键而不是 Unicode，非 ASCII 输入会明确失败。

多个客户端可以同时获取观察，但任意时刻只有一个 MCP Session 能持有控制权并发送输入动作。首个输入动作会隐式取得控制权，活跃调用自动维持；AI 无需执行取得或续租流程。控制权到期或 Session 断开时，设备会自动释放全部按键和鼠标按钮。

屏幕观察只返回本次请求实际采集到的当前帧。无视频信号、采集超时或采集设备不可用时会返回明确错误，不会把缓存画面冒充当前状态。

默认画面来源会自动发现 Linux 视频采集设备：只发现一个时自动使用，未发现时报告未连接，发现多个时要求管理员明确选择。配置固定设备时优先使用 `/dev/v4l/by-id/` 下的稳定路径，避免 USB 插拔后误选设备。

一级支持的画面来源直接使用 Linux V4L2，首版支持常见 USB 采集卡的 MJPEG 与 YUYV 输出，不依赖 FFmpeg。设备只提供其他格式时会明确失败，并由 `kvmctl doctor` 列出检测结果。

## 安全边界

拥有 `kvmctl` 控制权限等同于拥有被控主机的本地键盘、鼠标和屏幕访问权限。首个版本计划同时支持 stdio 和 MCP Streamable HTTP；HTTP 模式只能绑定回环地址，不接受局域网或公网监听地址。详细漏洞报告方式见 [SECURITY.md](SECURITY.md)。

截图默认只在内存中处理和返回，除非管理员通过 CLI 明确指定输出文件。日志不记录图像、`type_text` 内容或完整工具参数，只保留工具名、耗时、结果代码和匿名 Session 元数据。

长期运行的 `kvmctl` 服务使用专用非 root 用户。只有开机时创建 USB Gadget 与设置设备权限的一次性准备步骤使用 root；MCP 不提供提权或系统修复入口。

## 安装到 Linux 控制设备

下载与架构匹配的二进制并校验 `SHA256SUMS`，在仓库根目录显式执行：

```shell
sudo ./scripts/install.sh ./kvmctl-linux-armv7
sudoedit /etc/kvmctl/config.toml
sudo /usr/local/bin/kvmctl doctor
sudo systemctl enable --now kvmctl.service
```

安装脚本只复制文件、创建受限用户并刷新 systemd；它不会自动启用服务。Gadget 初始化由 root oneshot 服务执行，MCP 长期进程以 `kvmctl` 用户运行。接线与首次验收见[香橙派 One 指南](docs/hardware/orangepi-one.md)。

## 当前路线

1. 在到货的香橙派 One 上完成供电、UDC、三路 HID 与采集卡真实验收。
2. 完成外部 AI 端到端控制循环与 BIOS/UEFI 兼容性记录。
3. 发布首个带校验值的多架构版本。
4. 将其他具备 USB Device/OTG 能力的 Linux 板卡保持为实验性适配。

## 文档

- [领域语言](CONTEXT.md)
- [架构决策记录](docs/adr/)
- [v0.1 操作面](docs/spec/operations-v0.1.md)
- [配置说明](docs/configuration.md)
- [香橙派 One 接线与验收](docs/hardware/orangepi-one.md)
- [贡献指南](CONTRIBUTING.md)
- [安全策略](SECURITY.md)

## 发布形式

GitHub Release 工作流生成带 SHA-256 校验值的 `linux-armv7`、`linux-arm64` 和 `linux-amd64` 单文件程序；香橙派 One 使用一级支持的 armv7 构建，其他架构暂列实验性。仓库提供由用户明确执行的安装脚本，用于安装程序、systemd 服务、配置目录和设备权限，但不发布完整系统镜像。`kvmctl doctor` 用于检查 Linux、UDC、视频设备与权限状态。

系统安装默认读取 `/etc/kvmctl/config.toml`，开发和虚拟机测试可通过显式 `--config` 指定其他路径。程序不自动读取当前目录配置，MCP 不提供配置写入工具；配置更新只有在完整校验通过后才原子生效。

## 许可证

本项目采用 [Apache License 2.0](LICENSE)。

---

`kvmctl` is an open-source, AI-facing KVM control service. It exposes on-demand V4L2 screen capture and atomic USB Gadget HID input through a CLI and MCP stdio/Streamable HTTP server. Orange Pi One is the sole tier-1 hardware target; other Linux boards are experimental.
