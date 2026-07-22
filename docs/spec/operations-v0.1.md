# kvmctl v0.1 操作面

本文定义首版实现的 MCP 工具、只供管理员使用的 CLI，以及明确不提供的能力。工具是否出现在 MCP `tools/list` 中由当前有效配置决定；瞬时硬件状态通过 `get_status` 和结构化错误表达。

## MCP 工具

### 始终可用

#### `get_status`

读取服务版本、已配置能力、画面来源、键盘、绝对与相对指针、被控端连接状态及当前控制权状态。返回内容不得包含密钥。

- 只读：是
- 幂等：是
- 参数：无

### 屏幕观察

仅在配置了画面来源时公布。

#### `get_screen`

采集当前屏幕；没有当前帧时明确失败，不返回历史缓存。

参数：

- `view`: `original | coordinates | both`，默认 `coordinates`
- `format`: `jpeg | png`，默认 `jpeg`
- `quality`: JPEG 质量 `60～95`，默认 `85`；PNG 时禁止提供

结构化结果包括实际宽高、采集时间、信号状态和返回的图像类型。坐标视图采用左上角 `(0,0)`、右下角 `(1000,1000)` 的固定刻度。

- 只读：是
- 幂等：否；每次取得新的当前画面

### 键盘

仅在配置了键盘 HID 时公布。

#### `key_press`

同时按下指定按键，保持有限时间后自动全部释放。

参数：

- `keys`: 1～6 个非修饰键，可同时包含 `ctrl`、`shift`、`alt`、`meta`
- `hold_ms`: `20～5000`，默认 `80`

组合键示例：`["ctrl", "alt", "delete"]`。不提供独立的 `key_down` 或 `key_up`。

#### `type_text`

按照 US 键盘布局逐字输入可打印 ASCII 文本。

参数：

- `text`: 1～1024 个可打印 ASCII 字符
- `interval_ms`: 字符间隔 `0～500`，默认 `20`

换行、Tab、退格等控制键必须使用 `key_press`；非 ASCII 文本明确失败。

### 绝对指针

仅在配置了绝对指针 HID 时公布。坐标范围固定为 `0～1000`，与实际分辨率无关，操作不携带观察对象，也不要求预先截图。

#### `mouse_move_absolute`

移动到指定绝对位置。

- `x`: `0～1000`
- `y`: `0～1000`

#### `mouse_click_absolute`

移动到指定绝对位置并完成点击。

- `x`: `0～1000`
- `y`: `0～1000`
- `button`: `left | right | middle`
- `clicks`: `1～3`，默认 `1`
- `interval_ms`: 多次点击间隔 `50～1000`，默认 `120`

#### `mouse_drag_absolute`

在一个原子操作中移动到起点、按下按钮、平滑移动到终点并释放。

- `from_x`, `from_y`: `0～1000`
- `to_x`, `to_y`: `0～1000`
- `button`: `left | right | middle`
- `duration_ms`: `50～5000`，默认 `500`

### 相对指针

仅在配置了相对指针 HID 时公布。移动量是 HID 计数，不承诺对应固定像素距离。

#### `mouse_move_relative`

从被控端当前光标位置移动指定计数。

- `dx`: `-32767～32767`
- `dy`: `-32767～32767`

`dx` 与 `dy` 不能同时为零。服务把超出单份 HID 报告范围的移动自动拆分。

#### `mouse_click`

在当前光标位置完成点击。

- `button`: `left | right | middle`
- `clicks`: `1～3`，默认 `1`
- `interval_ms`: 多次点击间隔 `50～1000`，默认 `120`

#### `mouse_drag_relative`

在一个原子操作中按下按钮、相对移动并释放。

- `dx`: `-32767～32767`
- `dy`: `-32767～32767`
- `button`: `left | right | middle`
- `duration_ms`: `50～5000`，默认 `500`

#### `mouse_scroll`

发送垂直滚轮事件。

- `delta`: `-100～100`，不能为零

正值向上，负值向下。首版不承诺水平滚轮，以保留更好的固件兼容性。

### 控制与恢复

只要配置了任意输入能力就公布。首个输入动作隐式取得控制权，同一 MCP Session 的活跃调用自动维持控制权。

#### `release_all`

立即释放全部键盘按键和鼠标按钮。操作幂等，并在任何输入动作失败、控制权到期或 Session 断开时由服务自动执行。

#### `release_control`

当前持有者主动释放全部输入状态并交还控制权。普通 AI 控制循环无需调用。

### 浏览器控制会话

只在统一 `serve` 模式中公布。控制设备全局最多存在一个待兑换入口或浏览器会话。

#### `browser_session_open`

创建十分钟内可单次兑换的浏览器入口，返回 `entry_url` 和 `expires_at`。已有入口或会话时返回 `BROWSER_SESSION_BUSY`、当前状态以及可推导时的 `retry_at`；不返回或复用既有令牌，也不自动终止操作者。

#### `browser_session_status`

返回 `none | entry_pending | connected | reconnecting`，不包含入口令牌、会话 Cookie 或其他秘密。

#### `browser_session_close`

撤销待兑换入口或立即终止活动连接，并释放全部浏览器输入状态。操作幂等。

## MCP 资源

- `kvmctl://guide`: 推荐控制流程和安全边界
- `kvmctl://key-names`: `key_press` 支持的规范按键名称
- `kvmctl://errors`: 结构化错误、原因和恢复建议

工具定义必须能够独立使用，不能假设 MCP 客户端一定会读取资源。

## 管理 CLI

CLI 与 MCP 使用同一个控制核心。管理功能不作为 MCP 工具暴露。

- `kvmctl version`: 显示版本、提交和构建目标
- `kvmctl status [--json]`: 显示能力与瞬时状态
- `kvmctl doctor [--json]`: 检查内核、UDC、视频设备、HID 节点和权限
- `kvmctl devices screen`: 列出可用画面来源及稳定设备路径
- `kvmctl config validate --file <path>`: 验证配置但不修改系统
- `kvmctl mcp --transport stdio`: 启动 stdio MCP Server
- `kvmctl mcp --transport http --listen <loopback>`: 启动 Streamable HTTP MCP Server；拒绝非回环地址
- `kvmctl serve --listen <loopback>`: 在同一回环地址提供 `/mcp` 和 `/control/*` 统一服务
- `kvmctl screen ...`: 取得屏幕图像
- `kvmctl key press ...`: 执行原子按键
- `kvmctl text type ...`: 输入 US ASCII 文本
- `kvmctl mouse ...`: 执行与 MCP 对应的绝对或相对鼠标动作
- `kvmctl release-all`: 本机紧急释放全部输入状态

一级支持的画面来源直接通过 Linux V4L2 读取 MJPEG 或 YUYV，不调用 Shell 或 FFmpeg。`devices screen` 与 `doctor` 应列出采集设备公布的格式；没有受支持格式时明确失败。

安装、卸载和 systemd 生命周期由仓库提供的显式脚本及系统工具负责，不由 MCP 执行。

## 配置入口

- 系统默认：`/etc/kvmctl/config.toml`
- 显式覆盖：`kvmctl --config <path> ...`
- 不自动读取当前工作目录中的配置
- MCP 不提供读取秘密或修改配置的工具
- 热加载先完整校验候选配置，成功后原子替换；失败时保留上一份有效配置

## 结构化错误

- `INVALID_ARGUMENT`: 参数类型、范围、枚举或字符集错误
- `CAPABILITY_NOT_CONFIGURED`: 当前有效配置未启用该能力
- `DEVICE_MISSING`: 所需视频或 HID 设备节点不存在
- `NO_VIDEO_SIGNAL`: 画面来源存在但当前没有有效信号
- `CAPTURE_TIMEOUT`: 本次截图未在时限内完成
- `HOST_DISCONNECTED`: 被控端尚未枚举或已经断开输入设备
- `CONTROL_BUSY`: 另一 MCP Session 正在控制
- `BROWSER_SESSION_BUSY`: 已有浏览器入口或控制会话占用全局槽位
- `INPUT_FAILED`: HID 报告写入失败
- `OPERATION_TIMEOUT`: 原子输入动作超过时限并已执行释放
- `INTERNAL_ERROR`: 未归类内部错误；不得把它伪装成成功

错误结果应包含稳定代码、简短说明和可执行的恢复建议，不返回秘密或不必要的系统路径。

## 隐私与日志

- MCP 截图只在内存中处理和返回，默认不写磁盘
- 只有 CLI 明确指定输出路径时才保存截图
- 日志不记录图像、编码数据、`type_text` 内容或完整工具参数
- 普通日志不记录具体按键与坐标
- 可以记录工具名、耗时、结果代码和匿名 Session 标识
- 失败时不自动生成诊断截图
- 日志写入 stderr，保留周期由服务管理器决定

## 明确不提供

- 任意 Shell 命令执行
- 文件上传或下载
- 被控主机剪贴板与 Unicode 注入
- 面向 MCP 的未经限制 `key_down/key_up`、`mouse_down/mouse_up`；有状态输入只存在于受保护的单一浏览器会话
- 电源、复位或 ATX GPIO 控制
- 面向 MCP 的连续视频流；浏览器会话使用固定上限的实时 MJPEG
- 配置修改、软件升级或系统服务管理的 MCP 工具
- 直接监听局域网或公网地址的 MCP HTTP 模式

## 运行权限

- 一次性开机准备以 root 创建 USB Gadget 并设置设备所有权
- 长期运行的 MCP 服务使用专用非 root 用户
- 视频和 HID 访问通过设备组及 udev 权限授予
- `doctor` 只报告缺失权限和修复说明，不自动提权修改系统
