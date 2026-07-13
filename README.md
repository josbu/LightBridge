<div align="center">

<img src="frontend/public/logo.png" alt="LightBridge" width="120" />

# LightBridge

**A self-hosted, multi-provider AI API gateway.**

Bring your own Anthropic, OpenAI, and Gemini accounts together behind one unified, OpenAI/Anthropic/Gemini-compatible endpoint — with account pooling, smart failover, usage billing, and a full admin console.

[![Release](https://img.shields.io/github/v/release/WilliamWang1721/LightBridge?style=flat-square)](https://github.com/WilliamWang1721/LightBridge/releases)
[![License: LGPL-3.0](https://img.shields.io/badge/License-LGPL--3.0-blue.svg?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](backend/go.mod)
[![Vue 3](https://img.shields.io/badge/Vue-3-4FC08D?style=flat-square&logo=vuedotjs)](frontend/package.json)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker)](deploy/DOCKER.md)

English · [简体中文](README_CN.md) · [日本語](README_JA.md)

</div>

---

## What is LightBridge?

LightBridge sits between your applications and upstream AI providers. You register your provider accounts (API keys or OAuth) once, and LightBridge exposes a single set of standard-compatible endpoints. It automatically picks a healthy account, balances load across the pool, retries on failure, tracks token usage, and bills your users — all configurable from a modern web console.

It speaks the native dialects of all three major providers, so existing SDKs and tools work without code changes:

| Protocol | Endpoint | Compatible with |
|----------|----------|-----------------|
| **Anthropic** | `POST /v1/messages` · `/v1/messages/count_tokens` | Claude SDK, Claude Code, Anthropic clients |
| **OpenAI** | `POST /v1/chat/completions` · `/v1/responses` | OpenAI SDK, Codex, any OpenAI-compatible client |
| **Gemini** | `POST /v1beta/models/{model}:generateContent` | Google GenAI SDK, Gemini CLI |

## Features

**🔌 Multi-provider gateway**
- Unified Anthropic / OpenAI / Gemini compatible APIs from a single host
- Custom providers for any OpenAI-compatible upstream
- Per-model mapping and whitelisting

**⚖️ Account pooling & reliability**
- Pool multiple accounts per provider with priority, weight, and load factor
- Automatic load balancing and health-aware account selection
- Failover loop that retries failed requests against other healthy accounts
- Channel monitoring with a 30-day GitHub-style availability grid

**🔐 Flexible authentication**
- API keys (with API key auth) and OAuth for Gemini (Code Assist, AI Studio, API Key)
- User login via email, LinuxDO, Google/GitHub, WeChat, DingTalk, and generic OIDC

**💳 Billing & multi-tenancy**
- Per-user API keys, quotas, and concurrency limits
- Token-based usage tracking with configurable pricing and billing multipliers
- Stripe / Airwallex payment integration and invitation rebates

**🛡️ Privacy & security**
- Built-in privacy filter with redaction rules (IPv6, JWT, PEM keys, AWS/GitHub/Slack tokens, credit cards, and more), scoped by user and channel
- Content moderation hooks and TLS fingerprint simulation for upstream requests

**📊 Admin console**
- Customizable, drag-and-drop dashboard cards: availability, concurrency, throughput, latency, error trends, token usage, model distribution, and more
- Bulk user and account management, announcements, alerts, and system logs
- Unified progressive feature registration and control plane

## Quick Start

The fastest way to run LightBridge is Docker Compose. The script generates secure secrets and data directories for you.

```bash
curl -sSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/docker-deploy.sh | bash
```

Then start the stack and open the web UI:

```bash
docker compose -f docker-compose.local.yml up -d

# If the admin password was auto-generated, find it in the logs:
docker compose -f docker-compose.local.yml logs LightBridge | grep "admin password"
```

Open `http://localhost:8080` and sign in. See [`deploy/README.md`](deploy/README.md) for manual deployment, environment variables, Gemini OAuth setup, and migration details.

## Installation

LightBridge supports two deployment methods:

| Method | Best for | Setup |
|--------|----------|-------|
| **Docker Compose** | Quick setup, all-in-one | Auto-setup, no wizard needed |
| **Binary + systemd** | Production servers | Web-based setup wizard |

### Binary install (systemd)

```bash
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash
```

After the service starts, open the setup wizard at `http://YOUR_SERVER_IP:8080`.

**Prerequisites:** Linux (Ubuntu 20.04+, Debian 11+, CentOS 8+), PostgreSQL 14+, Redis 6+, systemd.

### Upgrade

```bash
# Upgrade to the latest release
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- upgrade

# Install or roll back to a specific version
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- upgrade -v v0.2.3
```

### Migrate from Sub2API

If your server still runs a legacy Sub2API binary deployment:

```bash
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- migrate -v v0.2.3
```

The migration backs up the legacy deployment, copies config/runtime files into the LightBridge layout, and switches the systemd service over. For a full data migration (accounts, providers, database), see the `sub2api-full-migrate.sh` section in [`deploy/README.md`](deploy/README.md). Backups are written to `/opt/LightBridge-migration-backups/<timestamp>`.

## Usage

Once you've added at least one provider account and created an API key in the console, point any compatible client at your LightBridge host.

**Anthropic-compatible:**

```bash
curl http://localhost:8080/v1/messages \
  -H "x-api-key: $LIGHTBRIDGE_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

**OpenAI-compatible:**

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $LIGHTBRIDGE_API_KEY" \
  -H "content-type: application/json" \
  -d '{
    "model": "gpt-5.3",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

## Architecture

| Layer | Tech |
|-------|------|
| **Backend** | Go 1.26 · Gin · Ent ORM · Wire (DI) |
| **Frontend** | Vue 3 · Vite · Pinia · Vue Router · Chart.js (pnpm) |
| **Data** | PostgreSQL 16 · Redis |
| **Delivery** | GoReleaser · Docker / GHCR · systemd |

```
LightBridge/
├── backend/
│   ├── cmd/server/          # Main entrypoint
│   ├── ent/                 # Ent ORM models & schema
│   ├── internal/
│   │   ├── handler/         # HTTP handlers (gateway, admin, auth)
│   │   ├── service/         # Business logic
│   │   ├── repository/      # Data access
│   │   ├── outbound/        # Upstream provider clients
│   │   ├── modules/         # Managed provider runtime
│   │   └── server/          # Routing & middleware
│   └── migrations/          # SQL migrations
├── frontend/                # Vue 3 admin console
└── deploy/                  # Docker, systemd, install scripts
```

## Development

See [`DEV_GUIDE.md`](DEV_GUIDE.md) for the full local setup, common pitfalls, and the PR checklist.

```bash
# Backend
cd backend
go run ./cmd/server/        # Run the server
go generate ./ent           # Regenerate Ent code after schema changes
go test -tags=unit ./...    # Unit tests
go test -tags=integration ./...

# Frontend (use pnpm, not npm)
cd frontend
pnpm install
pnpm dev                    # Dev server
pnpm build                  # Production build
```

## Contributing

Contributions are welcome. Please read [`CLA.md`](CLA.md) before submitting a pull request, and follow the PR checklist in [`DEV_GUIDE.md`](DEV_GUIDE.md). Releases follow [`docs/RELEASE_PROCESS.md`](docs/RELEASE_PROCESS.md).

## Acknowledgements

LightBridge references or uses code and implementation ideas from the following open-source projects:

- [Sub2API](https://github.com/Wei-Shaw/sub2api)
- [New API](https://github.com/QuantumNous/new-api)

## License

LightBridge is licensed under the [GNU Lesser General Public License v3.0](LICENSE).

## Links

- [GitHub Releases](https://github.com/WilliamWang1721/LightBridge/releases)
- [LinuxDO](https://linux.do/) — a friendly developer community
