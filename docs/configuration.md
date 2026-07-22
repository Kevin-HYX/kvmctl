# 配置

系统服务只读取 `/etc/kvmctl/config.toml`。其他路径必须通过全局 `--config <path>` 显式指定；程序不会搜索当前目录，MCP 也不提供配置读取或修改工具。

安装后先执行：

```shell
kvmctl config validate --file /etc/kvmctl/config.toml
kvmctl --config /etc/kvmctl/config.toml status
kvmctl --config /etc/kvmctl/config.toml doctor
```

完整示例位于 [`packaging/config.example.toml`](../packaging/config.example.toml)。

## 画面来源

```toml
[screen]
enabled = true
device = ""
capture_timeout = "3s"
warmup_frames = 90
```

- `device` 为空：优先扫描 `/dev/v4l/by-id/`。恰好一个设备时自动使用；零个时报告 `DEVICE_MISSING`；多个时要求显式选择。
- `device` 非空：必须是 `/dev/` 下的明确路径，推荐 `kvmctl devices screen` 输出的稳定 by-id 路径。
- 一级路径只接受设备公布的 MJPEG 或 YUYV，直接通过 V4L2 读取。
- `capture_timeout` 范围为 `100ms` 到 `30s`。
- `warmup_frames` 控制每次打开采集流后先读取并释放多少帧，再返回当前画面；默认 `90`，范围为 `0` 到 `600`。启动后立即提供有效画面的设备可显式设为 `0`。

## HID

默认 Gadget 初始化脚本创建：

- `/dev/hidg0`：8 字节键盘报告；
- `/dev/hidg1`：6 字节绝对指针报告；
- `/dev/hidg2`：4 字节相对指针报告。

禁用某项能力时，其 MCP 工具不会出现在 `tools/list`。设备节点暂时消失时，工具仍然存在，但 `get_status` 显示 `device_missing`，调用返回结构化错误。

## MCP

```toml
[mcp]
control_lease = "30s"
```

控制租约在首个输入动作时隐式取得。观察不受控制权限制；另一个 Session 的输入会得到 `CONTROL_BUSY`。HTTP 模式只接受显式回环监听地址。

## 统一服务与浏览器入口

```toml
[server]
public_origin = "https://kvm.example.com:18443"

[server.webrtc]
enabled = true
stun_urls = ["stun:stun.cloudflare.com:3478", "stun:stun.l.google.com:19302"]
connect_timeout = "10s"
```

浏览器实时控制固定要求采集设备提供 `1920×1080、30 FPS` 的 MJPEG（优先）或 YUYV 模式；不满足时服务明确报告能力不可用，不会把低分辨率画面标记为 1080p。

生产服务使用：

```shell
kvmctl --config /etc/kvmctl/config.toml serve --listen 127.0.0.1:8080
```

同一回环监听器提供 `/mcp`、`/control/enter`、`/control/`、`/control/webrtc/*` 和 `/control/ws`。`public_origin` 必须是无路径、查询参数、Fragment 或用户信息的 HTTPS Origin；它只用于生成 MCP 返回的临时浏览器入口并严格校验浏览器信令 `Origin`，不会让 kvmctl 直接监听公网。

WebRTC 默认启用，并通过两个公共 STUN 服务发现可用于 UDP 直连的公网映射。`stun_urls = []` 只禁用公网候选发现，不禁用局域网 host 候选；首版不支持 TURN。`connect_timeout` 范围为 `1s` 到 `30s`。这些设置变更后需要重启服务。

浏览器入口使用 256 位安全随机令牌，十分钟内只能兑换一次。兑换后设置 Secure、HttpOnly、SameSite=Strict 的非持久 Cookie；活动连接不设固定最长时间，断线后保留六十秒供原浏览器恢复。入口请求的边缘访问日志不得记录查询参数。

Linux 服务收到 `SIGHUP` 时会先完整解析和校验候选配置，再原子替换控制器；失败时保留上一份有效配置。为保证已连接 MCP Session 的 `tools/list` 和浏览器 Origin 稳定，能力开关、`control_lease` 或 `server` 配置改变需要重启服务，设备路径、采集超时与预热帧数可以热更新。
