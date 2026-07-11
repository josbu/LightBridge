# ADMIN_PAYMENT_INTEGRATION_API

> 单文件中英双语文档 / Single-file bilingual documentation (Chinese + English)

---

## 中文

### 目标
本文档用于对接外部支付系统（如 `LightBridgepay`）与 LightBridge 的 Admin API，覆盖：
- 支付成功后充值
- 用户查询
- 人工余额修正
- 前端购买页参数透传

### 基础地址
- 生产：`https://<your-domain>`
- Beta：`http://<your-server-ip>:8084`

### 认证
推荐使用：
- `x-api-key: admin-<64hex>`
- `Content-Type: application/json`
- 幂等接口额外传：`Idempotency-Key`

说明：管理员 JWT 也可访问 admin 路由，但服务间调用建议使用 Admin API Key。

### 1) 一步完成创建并兑换
`POST /api/v1/admin/redeem-codes/create-and-redeem`

用途：原子完成“创建兑换码 + 兑换到指定用户”。

请求头：
- `x-api-key`
- `Idempotency-Key`

请求体示例：
```json
{
  "code": "s2p_cm1234567890",
  "type": "balance",
  "value": 100.0,
  "user_id": 123,
  "notes": "LightBridgepay order: cm1234567890"
}
```

幂等语义：
- 同 `code` 且 `used_by` 一致：`200`
- 同 `code` 但 `used_by` 不一致：`409`
- 缺少 `Idempotency-Key`：`400`（`IDEMPOTENCY_KEY_REQUIRED`）

curl 示例：
```bash
curl -X POST "${BASE}/api/v1/admin/redeem-codes/create-and-redeem" \
  -H "x-api-key: ${KEY}" \
  -H "Idempotency-Key: pay-cm1234567890-success" \
  -H "Content-Type: application/json" \
  -d '{
    "code":"s2p_cm1234567890",
    "type":"balance",
    "value":100.00,
    "user_id":123,
    "notes":"LightBridgepay order: cm1234567890"
  }'
```

### 2) 查询用户（可选前置校验）
`GET /api/v1/admin/users/:id`

```bash
curl -s "${BASE}/api/v1/admin/users/123" \
  -H "x-api-key: ${KEY}"
```

### 3) 余额调整（已有接口）
`POST /api/v1/admin/users/:id/balance`

用途：人工补偿 / 扣减，支持 `set` / `add` / `subtract`。

请求体示例（扣减）：
```json
{
  "balance": 100.0,
  "operation": "subtract",
  "notes": "manual correction"
}
```

```bash
curl -X POST "${BASE}/api/v1/admin/users/123/balance" \
  -H "x-api-key: ${KEY}" \
  -H "Idempotency-Key: balance-subtract-cm1234567890" \
  -H "Content-Type: application/json" \
  -d '{
    "balance":100.00,
    "operation":"subtract",
    "notes":"manual correction"
  }'
```

### 4) 购买页 / 自定义页面嵌入协议
LightBridge 打开用户侧自定义页面 iframe 时，只会在 URL 中追加非敏感上下文：
- `user_id`
- `theme`（`light` / `dark`）
- `lang`（例如 `zh` / `en`）
- `ui_mode`（固定 `embedded`）
- `src_host`
- `src_url`（仅包含 LightBridge 的 origin 与 pathname，不包含 query/hash）

登录 JWT **不会**进入 iframe URL，也不会直接交给外部页面。LightBridge 会先通过同源接口 `POST /api/v1/auth/embed-token` 换取一个 5 分钟有效的专用 token，再通过 `postMessage` 发送给 iframe。该 token：
- scope 固定为 `payment_embed`；
- audience 绑定 iframe 的精确 origin；
- 服务端只允许访问 `/api/v1/payment` 与 `/api/v1/payment/**`；
- 不能访问普通用户、管理员、OAuth 或签发 token 的接口；
- 不能被旧 access-token 刷新流程升级为普通登录 token。

这样可避免登录凭据进入浏览器历史、反向代理日志、统计系统、Referrer 或第三方页面持久化存储。

示例 iframe URL：
```text
https://pay.example.com/pay?user_id=123&theme=light&lang=zh&ui_mode=embedded&src_host=https%3A%2F%2Flightbridge.example.com
```

嵌入页应在消息监听器准备好后向父窗口发送 ready 消息：
```js
window.parent.postMessage(
  { type: 'lightbridge:embed-ready' },
  'https://lightbridge.example.com',
)
```

LightBridge 会确认消息来源正是当前 iframe，并要求目标使用 HTTPS（本地开发的 localhost/127.0.0.1/::1 允许 HTTP），然后向 iframe 的**精确 origin**发送认证消息：
```js
window.addEventListener('message', (event) => {
  if (event.origin !== 'https://lightbridge.example.com') return
  if (event.data?.type !== 'lightbridge:embed-auth') return
  if (event.data?.version !== 1) return
  if (event.data?.scope !== 'payment_embed') return

  const { token, expires_at, user_id, theme, lang, src_host } = event.data
  // token 只保存在内存中；不要写入 URL、日志或 localStorage。
  // token 到期前可再次发送 lightbridge:embed-ready 请求刷新。
})
```

认证消息格式：
```json
{
  "type": "lightbridge:embed-auth",
  "version": 1,
  "token": "<short-lived-payment-embed-jwt>",
  "scope": "payment_embed",
  "expires_at": 1783766400000,
  "user_id": 123,
  "theme": "light",
  "lang": "zh",
  "src_host": "https://lightbridge.example.com"
}
```

嵌入页调用支付 API 时使用：
```js
await fetch('https://lightbridge.example.com/api/v1/payment/config', {
  headers: { Authorization: `Bearer ${token}` },
})
```

浏览器会自动附带 iframe 页面的 `Origin`。LightBridge 会把该 Origin 与 token audience 精确比较，因此部署时还需要把支付页 origin 加入 LightBridge 的 CORS allowlist。服务端脚本可以伪造 Origin，所以 audience 绑定主要用于浏览器隔离；真正的最小权限仍由 `payment_embed` scope 和支付路由白名单保证。

“新标签页打开”不会传递任何 LightBridge token；外部页面应使用自己的登录流程或服务端专用支付会话。

### 5) 失败处理建议
- 支付成功与充值成功分状态落库
- 回调验签成功后立即标记“支付成功”
- 支付成功但充值失败的订单允许后续重试
- 重试保持相同 `code`，并使用新的 `Idempotency-Key`

### 6) `doc_url` 配置建议
- 查看链接：`https://github.com/WilliamWang1721/LightBridge/blob/main/ADMIN_PAYMENT_INTEGRATION_API.md`
- 下载链接：`https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/ADMIN_PAYMENT_INTEGRATION_API.md`

---

## English

### Purpose
This document describes the minimal LightBridge Admin API surface for external payment integrations (for example, `LightBridgepay`), including:
- Recharge after payment success
- User lookup
- Manual balance correction
- Purchase page query parameter forwarding

### Base URL
- Production: `https://<your-domain>`
- Beta: `http://<your-server-ip>:8084`

### Authentication
Recommended headers:
- `x-api-key: admin-<64hex>`
- `Content-Type: application/json`
- `Idempotency-Key` for idempotent endpoints

Note: Admin JWT can also access admin routes, but Admin API Key is recommended for server-to-server integration.

### 1) Create and Redeem in one step
`POST /api/v1/admin/redeem-codes/create-and-redeem`

Use case: atomically create a redeem code and redeem it to a target user.

Headers:
- `x-api-key`
- `Idempotency-Key`

Request body:
```json
{
  "code": "s2p_cm1234567890",
  "type": "balance",
  "value": 100.0,
  "user_id": 123,
  "notes": "LightBridgepay order: cm1234567890"
}
```

Idempotency behavior:
- Same `code` and same `used_by`: `200`
- Same `code` but different `used_by`: `409`
- Missing `Idempotency-Key`: `400` (`IDEMPOTENCY_KEY_REQUIRED`)

curl example:
```bash
curl -X POST "${BASE}/api/v1/admin/redeem-codes/create-and-redeem" \
  -H "x-api-key: ${KEY}" \
  -H "Idempotency-Key: pay-cm1234567890-success" \
  -H "Content-Type: application/json" \
  -d '{
    "code":"s2p_cm1234567890",
    "type":"balance",
    "value":100.00,
    "user_id":123,
    "notes":"LightBridgepay order: cm1234567890"
  }'
```

### 2) Query User (optional pre-check)
`GET /api/v1/admin/users/:id`

```bash
curl -s "${BASE}/api/v1/admin/users/123" \
  -H "x-api-key: ${KEY}"
```

### 3) Balance Adjustment (existing API)
`POST /api/v1/admin/users/:id/balance`

Use case: manual correction with `set` / `add` / `subtract`.

Request body example (`subtract`):
```json
{
  "balance": 100.0,
  "operation": "subtract",
  "notes": "manual correction"
}
```

```bash
curl -X POST "${BASE}/api/v1/admin/users/123/balance" \
  -H "x-api-key: ${KEY}" \
  -H "Idempotency-Key: balance-subtract-cm1234567890" \
  -H "Content-Type: application/json" \
  -d '{
    "balance":100.00,
    "operation":"subtract",
    "notes":"manual correction"
  }'
```

### 4) Purchase / custom-page embed protocol
When LightBridge opens a user-facing custom page iframe, it appends only non-sensitive context to the URL:
- `user_id`
- `theme` (`light` / `dark`)
- `lang` (for example `zh` / `en`)
- `ui_mode` (fixed: `embedded`)
- `src_host`
- `src_url` (LightBridge origin and pathname only; query/hash are excluded)

The login JWT is **never** placed in the iframe URL or handed directly to the external page. LightBridge first exchanges it through the same-origin `POST /api/v1/auth/embed-token` endpoint for a dedicated five-minute token and sends that token with `postMessage`. The token:
- has the fixed `payment_embed` scope;
- is audience-bound to the iframe's exact origin;
- is accepted server-side only on `/api/v1/payment` and `/api/v1/payment/**`;
- cannot access normal user, admin, OAuth, or token-issuing endpoints; and
- cannot be upgraded into a normal login token through the legacy access-token refresh flow.

This keeps login credentials out of browser history, reverse-proxy logs, analytics systems, referrers, and third-party persistent storage.

Example iframe URL:
```text
https://pay.example.com/pay?user_id=123&theme=light&lang=zh&ui_mode=embedded&src_host=https%3A%2F%2Flightbridge.example.com
```

After installing its message listener, the embedded page should announce readiness to the parent:
```js
window.parent.postMessage(
  { type: 'lightbridge:embed-ready' },
  'https://lightbridge.example.com',
)
```

LightBridge verifies that the message came from the current iframe, requires HTTPS (HTTP is allowed only for localhost/127.0.0.1/::1 development origins), and sends an authentication message to the iframe's **exact origin**:
```js
window.addEventListener('message', (event) => {
  if (event.origin !== 'https://lightbridge.example.com') return
  if (event.data?.type !== 'lightbridge:embed-auth') return
  if (event.data?.version !== 1) return
  if (event.data?.scope !== 'payment_embed') return

  const { token, expires_at, user_id, theme, lang, src_host } = event.data
  // Keep the token in memory only. Never put it in a URL, log, or localStorage.
  // Send lightbridge:embed-ready again before expiry to request a refreshed token.
})
```

Authentication message schema:
```json
{
  "type": "lightbridge:embed-auth",
  "version": 1,
  "token": "<short-lived-payment-embed-jwt>",
  "scope": "payment_embed",
  "expires_at": 1783766400000,
  "user_id": 123,
  "theme": "light",
  "lang": "zh",
  "src_host": "https://lightbridge.example.com"
}
```

Use the token for payment APIs:
```js
await fetch('https://lightbridge.example.com/api/v1/payment/config', {
  headers: { Authorization: `Bearer ${token}` },
})
```

The browser automatically sends the iframe page's `Origin`. LightBridge compares it exactly with the token audience, so the payment-page origin must also be present in LightBridge's CORS allowlist. A server-side client can forge an Origin header; audience binding is therefore a browser-isolation control, while the `payment_embed` scope and payment-route allowlist provide the actual least-privilege boundary.

Opening the page in a new tab transfers no LightBridge token. The external page must use its own login flow or a dedicated server-issued payment session.

### 5) Failure handling recommendations
- Persist payment success and recharge success as separate states
- Mark payment as successful immediately after verified callback
- Allow retry for orders with payment success but recharge failure
- Keep the same `code` for retry, and use a new `Idempotency-Key`

### 6) Recommended `doc_url`
- View URL: `https://github.com/WilliamWang1721/LightBridge/blob/main/ADMIN_PAYMENT_INTEGRATION_API.md`
- Download URL: `https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/ADMIN_PAYMENT_INTEGRATION_API.md`
