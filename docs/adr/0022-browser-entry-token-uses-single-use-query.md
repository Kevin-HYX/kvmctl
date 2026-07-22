# 浏览器入口令牌通过一次性查询参数兑换

MCP 返回的浏览器入口把一次性随机令牌作为 `/control/enter` 的 URL 查询参数。服务只在该端点接受入口令牌，验证成功后立即销毁令牌、设置 Secure、HttpOnly、SameSite 会话 Cookie，并以 `303` 跳转到不含令牌的 `/control/`；无效、过期和已使用令牌产生相同响应。入口请求的各层日志不得记录查询参数，响应设置 `Referrer-Policy: no-referrer`，控制页不得加载第三方资源。后续连接和六十秒内的恢复只接受会话 Cookie，不再接受 URL 令牌。
