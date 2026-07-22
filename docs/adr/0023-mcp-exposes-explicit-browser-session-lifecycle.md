# MCP 显式暴露浏览器会话生命周期

MCP 使用三个独立工具管理浏览器通道：`browser_session_open` 返回一次性入口及到期时间，`browser_session_status` 返回当前生命周期状态，`browser_session_close` 撤销待兑换入口或结束活动会话并执行输入状态全释放。工具名和输入输出分别描述单一意图，不使用 `action` 参数复用一个万能工具，使智能体可以直接从 MCP 工具描述中发现正确用法。
