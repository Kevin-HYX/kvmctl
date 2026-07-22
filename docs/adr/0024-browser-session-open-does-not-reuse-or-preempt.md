# 重复开启浏览器会话返回结构化占用错误

全局会话槽位处于 `entry_pending`、`connected` 或 `reconnecting` 时，`browser_session_open` 返回 `BROWSER_SESSION_BUSY`，并包含当前状态以及可推导时的 `retry_at`。服务不保存或重新暴露可用入口明文，不自动覆盖待兑换入口，也不踢掉现有操作者；调用方必须等待、查询状态，或显式调用 `browser_session_close`。
