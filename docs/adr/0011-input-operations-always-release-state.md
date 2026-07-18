# 输入操作必须自动释放状态

首版不向 MCP 暴露任意的 `key_down/key_up` 或 `mouse_down/mouse_up`。按键、点击和拖拽都是有时间上限的原子操作，并在成功、失败、超时、控制权到期或 Session 断开时释放相关 HID 状态；有限长按由 `hold_ms` 表达，拖拽由单一工具完成。
