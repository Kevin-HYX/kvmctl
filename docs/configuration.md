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
```

- `device` 为空：优先扫描 `/dev/v4l/by-id/`。恰好一个设备时自动使用；零个时报告 `DEVICE_MISSING`；多个时要求显式选择。
- `device` 非空：必须是 `/dev/` 下的明确路径，推荐 `kvmctl devices screen` 输出的稳定 by-id 路径。
- 一级路径只接受设备公布的 MJPEG 或 YUYV，直接通过 V4L2 读取。
- `capture_timeout` 范围为 `100ms` 到 `30s`。

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

Linux 服务收到 `SIGHUP` 时会先完整解析和校验候选配置，再原子替换控制器；失败时保留上一份有效配置。为保证已连接 MCP Session 的 `tools/list` 稳定，能力开关或 `control_lease` 改变需要重启服务，设备路径与采集超时可以热更新。
