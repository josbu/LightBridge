<div align="center">

<img src="frontend/public/logo.png" alt="LightBridge" width="120" />

# LightBridge

**自托管的多供应商 AI API 网关。**

把你的 Anthropic、OpenAI、Gemini 账号汇聚到一个统一、且同时兼容 OpenAI / Anthropic / Gemini 协议的入口背后 —— 内置账号池、智能故障转移、用量计费,以及完整的管理控制台。

[![Release](https://img.shields.io/github/v/release/WilliamWang1721/LightBridge?style=flat-square)](https://github.com/WilliamWang1721/LightBridge/releases)
[![License: LGPL-3.0](https://img.shields.io/badge/License-LGPL--3.0-blue.svg?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](backend/go.mod)
[![Vue 3](https://img.shields.io/badge/Vue-3-4FC08D?style=flat-square&logo=vuedotjs)](frontend/package.json)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker)](deploy/DOCKER.md)

[English](README.md) · 简体中文 · [日本語](README_JA.md)

</div>

---

## LightBridge 是什么?

LightBridge 位于你的应用与上游 AI 供应商之间。你只需注册一次供应商账号(API Key 或 OAuth),LightBridge 就对外暴露一组标准协议兼容的接口。它会自动挑选健康的账号、在账号池之间均衡负载、失败时自动重试、统计 Token 用量并对用户计费 —— 全部都能在现代化的 Web 控制台里配置。

它原生支持三大供应商的协议方言,现有 SDK 和工具无需改代码即可使用:

| 协议 | 端点 | 兼容客户端 |
|------|------|-----------|
| **Anthropic** | `POST /v1/messages` · `/v1/messages/count_tokens` | Claude SDK、Claude Code、Anthropic 客户端 |
| **OpenAI** | `POST /v1/chat/completions` · `/v1/responses` | OpenAI SDK、Codex、各类 OpenAI 兼容客户端 |
| **Gemini** | `POST /v1beta/models/{model}:generateContent` | Google GenAI SDK、Gemini CLI |

## 功能特性

**🔌 多供应商网关**
- 单一主机对外提供 Anthropic / OpenAI / Gemini 兼容 API
- 支持自定义供应商(任意 OpenAI 兼容上游)
- 按模型映射与白名单

**⚖️ 账号池与高可用**
- 每个供应商可配置多账号,支持优先级、权重、负载因子
- 自动负载均衡与基于健康度的账号选择
- 故障转移循环:请求失败时自动切换到其它健康账号重试
- 渠道监控,提供 30 天 GitHub 风格的可用性网格

**🔐 灵活的身份认证**
- API Key(带 API Key 鉴权),Gemini 支持 OAuth(Code Assist / AI Studio / API Key)
- 用户登录支持邮箱、LinuxDO、Google/GitHub、微信、钉钉,以及通用 OIDC

**💳 计费与多租户**
- 按用户的 API Key、配额与并发限制
- 基于 Token 的用量统计,可配置定价与计费倍率
- 集成 Stripe / Airwallex 支付,支持邀请返利

**🛡️ 隐私与安全**
- 内置隐私过滤,提供脱敏规则(IPv6、JWT、PEM 私钥、AWS/GitHub/Slack Token、信用卡号等),可按用户和渠道维度生效
- 内容审核钩子,以及对上游请求的 TLS 指纹模拟

**📊 管理控制台**
- 可自定义、可拖拽排序的仪表盘卡片:可用性、并发、吞吐量、延迟、错误趋势、Token 用量、模型分布等
- 批量用户与账号管理、公告、告警、系统日志
- 统一的渐进式功能注册与控制面

## 快速开始

运行 LightBridge 最快的方式是 Docker Compose。脚本会自动为你生成安全密钥和数据目录。

```bash
curl -sSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/docker-deploy.sh | bash
```

然后启动服务并打开 Web 界面:

```bash
docker compose -f docker-compose.local.yml up -d

# 如果管理员密码是自动生成的,可在日志中查找:
docker compose -f docker-compose.local.yml logs LightBridge | grep "admin password"
```

浏览器打开 `http://localhost:8080` 登录。手动部署、环境变量、Gemini OAuth 配置与迁移细节见 [`deploy/README.md`](deploy/README.md)。

## 安装部署

LightBridge 支持两种部署方式:

| 方式 | 适用场景 | 初始化 |
|------|----------|--------|
| **Docker Compose** | 快速搭建、一体化 | 自动初始化,无需向导 |
| **二进制 + systemd** | 生产服务器 | Web 初始化向导 |

### 二进制安装(systemd)

```bash
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash
```

服务启动后,在浏览器打开 `http://服务器IP:8080` 进入初始化向导。

**前置要求:** Linux(Ubuntu 20.04+、Debian 11+、CentOS 8+)、PostgreSQL 14+、Redis 6+、systemd。

### 升级

```bash
# 升级到最新 Release
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- upgrade

# 安装或回退到指定版本
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- upgrade -v v0.2.3
```

### 从 Sub2API 迁移

如果服务器上仍是旧的 Sub2API 二进制部署:

```bash
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- migrate -v v0.2.3
```

迁移会备份旧部署、把配置和运行数据复制到 LightBridge 目录,并切换 systemd 服务。如需完整数据迁移(账号、供应商、数据库),见 [`deploy/README.md`](deploy/README.md) 中的 `sub2api-full-migrate.sh` 章节。备份写入 `/opt/LightBridge-migration-backups/<timestamp>`。

## 使用示例

在控制台中添加至少一个供应商账号并创建 API Key 后,把任意兼容客户端指向你的 LightBridge 主机即可。

**Anthropic 兼容:**

```bash
curl http://localhost:8080/v1/messages \
  -H "x-api-key: $LIGHTBRIDGE_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

**OpenAI 兼容:**

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $LIGHTBRIDGE_API_KEY" \
  -H "content-type: application/json" \
  -d '{
    "model": "gpt-5.3",
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

## 技术架构

| 层 | 技术栈 |
|----|--------|
| **后端** | Go 1.26 · Gin · Ent ORM · Wire(依赖注入) |
| **前端** | Vue 3 · Vite · Pinia · Vue Router · Chart.js(pnpm) |
| **数据** | PostgreSQL 16 · Redis |
| **交付** | GoReleaser · Docker / GHCR · systemd |

```
LightBridge/
├── backend/
│   ├── cmd/server/          # 主程序入口
│   ├── ent/                 # Ent ORM 模型与 Schema
│   ├── internal/
│   │   ├── handler/         # HTTP 处理器(网关、管理、认证)
│   │   ├── service/         # 业务逻辑
│   │   ├── repository/      # 数据访问层
│   │   ├── outbound/        # 上游供应商客户端
│   │   ├── modules/         # 托管 Provider 运行时
│   │   └── server/          # 路由与中间件
│   └── migrations/          # SQL 迁移脚本
├── frontend/                # Vue 3 管理控制台
└── deploy/                  # Docker、systemd、安装脚本
```

## 开发

完整的本地环境配置、常见坑点和 PR 检查清单见 [`DEV_GUIDE.md`](DEV_GUIDE.md)。

```bash
# 后端
cd backend
go run ./cmd/server/        # 运行服务器
go generate ./ent           # 修改 schema 后重新生成 Ent 代码
go test -tags=unit ./...    # 单元测试
go test -tags=integration ./...

# 前端(必须用 pnpm,不是 npm)
cd frontend
pnpm install
pnpm dev                    # 开发服务器
pnpm build                  # 生产构建
```

## 贡献

欢迎贡献代码。提交 PR 前请阅读 [`CLA.md`](CLA.md),并遵循 [`DEV_GUIDE.md`](DEV_GUIDE.md) 中的 PR 检查清单。版本发布流程见 [`docs/RELEASE_PROCESS.md`](docs/RELEASE_PROCESS.md)。

## 许可证

LightBridge 基于 [GNU Lesser General Public License v3.0](LICENSE) 许可。

## 友情链接

- [GitHub Releases](https://github.com/WilliamWang1721/LightBridge/releases)
- [LinuxDO](https://linux.do/) — 一个友好的开发者社区
