<div align="center">

<img src="frontend/public/logo.png" alt="LightBridge" width="120" />

# LightBridge

**セルフホスト型のマルチプロバイダー AI API ゲートウェイ。**

Anthropic・OpenAI・Gemini のアカウントを、OpenAI / Anthropic / Gemini 互換の統一エンドポイントの背後にまとめます。アカウントプール、スマートフェイルオーバー、利用量課金、そして完全な管理コンソールを備えています。

[![Release](https://img.shields.io/github/v/release/WilliamWang1721/LightBridge?style=flat-square)](https://github.com/WilliamWang1721/LightBridge/releases)
[![License: LGPL-3.0](https://img.shields.io/badge/License-LGPL--3.0-blue.svg?style=flat-square)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](backend/go.mod)
[![Vue 3](https://img.shields.io/badge/Vue-3-4FC08D?style=flat-square&logo=vuedotjs)](frontend/package.json)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker)](deploy/DOCKER.md)

[English](README.md) · [简体中文](README_CN.md) · 日本語

</div>

---

## LightBridge とは?

LightBridge はアプリケーションと上流の AI プロバイダーの間に位置します。プロバイダーのアカウント(API キーまたは OAuth)を一度登録するだけで、LightBridge が標準互換のエンドポイントを公開します。正常なアカウントを自動的に選択し、プール間で負荷分散を行い、失敗時にはリトライし、トークン使用量を記録してユーザーに課金します。これらすべてをモダンな Web コンソールから設定できます。

3 大プロバイダーのネイティブプロトコルに対応しているため、既存の SDK やツールをコード変更なしで利用できます:

| プロトコル | エンドポイント | 互換クライアント |
|-----------|---------------|-----------------|
| **Anthropic** | `POST /v1/messages` · `/v1/messages/count_tokens` | Claude SDK、Claude Code、Anthropic クライアント |
| **OpenAI** | `POST /v1/chat/completions` · `/v1/responses` | OpenAI SDK、Codex、各種 OpenAI 互換クライアント |
| **Gemini** | `POST /v1beta/models/{model}:generateContent` | Google GenAI SDK、Gemini CLI |

## 主な機能

**🔌 マルチプロバイダーゲートウェイ**
- 単一ホストから Anthropic / OpenAI / Gemini 互換 API を提供
- カスタムプロバイダー(任意の OpenAI 互換上流)に対応
- モデルごとのマッピングとホワイトリスト

**⚖️ アカウントプールと高可用性**
- プロバイダーごとに複数アカウントを設定し、優先度・重み・負荷係数をサポート
- 自動負荷分散とヘルスベースのアカウント選択
- フェイルオーバーループ:リクエスト失敗時に他の正常なアカウントへ自動的に切り替えてリトライ
- チャネル監視と 30 日間の GitHub 風可用性グリッド

**🔐 柔軟な認証**
- API キー(API キー認証付き)、Gemini は OAuth(Code Assist / AI Studio / API Key)に対応
- ユーザーログインはメール、LinuxDO、Google/GitHub、WeChat、DingTalk、汎用 OIDC に対応

**💳 課金とマルチテナント**
- ユーザーごとの API キー、クォータ、同時実行数の制限
- トークンベースの使用量追跡、価格と課金倍率を設定可能
- Stripe / Airwallex 決済連携、招待リベートに対応

**🛡️ プライバシーとセキュリティ**
- 組み込みのプライバシーフィルター(IPv6、JWT、PEM 秘密鍵、AWS/GitHub/Slack トークン、クレジットカード番号など)、ユーザーとチャネル単位で適用
- コンテンツモデレーションフック、上流リクエストへの TLS フィンガープリント模倣

**📊 管理コンソール**
- カスタマイズ可能・ドラッグ&ドロップ対応のダッシュボードカード:可用性、同時実行、スループット、レイテンシ、エラー傾向、トークン使用量、モデル分布など
- ユーザーとアカウントの一括管理、お知らせ、アラート、システムログ
- 組み込み機能をオンデマンドで有効/無効にできるモジュールマーケットプレイス

## クイックスタート

LightBridge を最も手軽に実行する方法は Docker Compose です。スクリプトが安全なシークレットとデータディレクトリを自動生成します。

```bash
curl -sSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/docker-deploy.sh | bash
```

その後、スタックを起動して Web UI を開きます:

```bash
docker compose -f docker-compose.local.yml up -d

# 管理者パスワードが自動生成された場合は、ログで確認できます:
docker compose -f docker-compose.local.yml logs LightBridge | grep "admin password"
```

ブラウザで `http://localhost:8080` を開いてログインします。手動デプロイ、環境変数、Gemini OAuth 設定、移行の詳細は [`deploy/README.md`](deploy/README.md) を参照してください。

## インストール

LightBridge は 2 つのデプロイ方法をサポートしています:

| 方法 | 適した用途 | 初期化 |
|------|-----------|--------|
| **Docker Compose** | 手早いセットアップ、オールインワン | 自動初期化、ウィザード不要 |
| **バイナリ + systemd** | 本番サーバー | Web 初期化ウィザード |

### バイナリインストール(systemd)

```bash
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash
```

サービス起動後、ブラウザで `http://YOUR_SERVER_IP:8080` を開いて初期化ウィザードを実行します。

**前提条件:** Linux(Ubuntu 20.04+、Debian 11+、CentOS 8+)、PostgreSQL 14+、Redis 6+、systemd。

### アップグレード

```bash
# 最新リリースへアップグレード
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- upgrade

# 特定バージョンのインストールまたはロールバック
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- upgrade -v v0.2.3
```

### Sub2API からの移行

サーバーに従来の Sub2API バイナリデプロイが残っている場合:

```bash
curl -fsSL https://raw.githubusercontent.com/WilliamWang1721/LightBridge/main/deploy/install.sh | sudo bash -s -- migrate -v v0.2.3
```

移行では従来のデプロイをバックアップし、設定・ランタイムファイルを LightBridge のレイアウトにコピーして、systemd サービスを切り替えます。完全なデータ移行(アカウント、プロバイダー、データベース)については [`deploy/README.md`](deploy/README.md) の `sub2api-full-migrate.sh` セクションを参照してください。バックアップは `/opt/LightBridge-migration-backups/<timestamp>` に書き込まれます。

## 使用例

コンソールで少なくとも 1 つのプロバイダーアカウントを追加し、API キーを作成したら、互換クライアントを LightBridge ホストに向けるだけです。

**Anthropic 互換:**

```bash
curl http://localhost:8080/v1/messages \
  -H "x-api-key: $LIGHTBRIDGE_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "こんにちは"}]
  }'
```

**OpenAI 互換:**

```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $LIGHTBRIDGE_API_KEY" \
  -H "content-type: application/json" \
  -d '{
    "model": "gpt-5.3",
    "messages": [{"role": "user", "content": "こんにちは"}]
  }'
```

## アーキテクチャ

| レイヤー | 技術スタック |
|---------|-------------|
| **バックエンド** | Go 1.26 · Gin · Ent ORM · Wire(DI) |
| **フロントエンド** | Vue 3 · Vite · Pinia · Vue Router · Chart.js(pnpm) |
| **データ** | PostgreSQL 16 · Redis |
| **デリバリー** | GoReleaser · Docker / GHCR · systemd |

```
LightBridge/
├── backend/
│   ├── cmd/server/          # メインエントリポイント
│   ├── ent/                 # Ent ORM モデルとスキーマ
│   ├── internal/
│   │   ├── handler/         # HTTP ハンドラ(ゲートウェイ、管理、認証)
│   │   ├── service/         # ビジネスロジック
│   │   ├── repository/      # データアクセス層
│   │   ├── outbound/        # 上流プロバイダークライアント
│   │   ├── modules/         # モジュールマーケットプレイス機能
│   │   └── server/          # ルーティングとミドルウェア
│   └── migrations/          # SQL マイグレーション
├── frontend/                # Vue 3 管理コンソール
└── deploy/                  # Docker、systemd、インストールスクリプト
```

## 開発

ローカル環境のセットアップ、よくある落とし穴、PR チェックリストの詳細は [`DEV_GUIDE.md`](DEV_GUIDE.md) を参照してください。

```bash
# バックエンド
cd backend
go run ./cmd/server/        # サーバーを実行
go generate ./ent           # スキーマ変更後に Ent コードを再生成
go test -tags=unit ./...    # ユニットテスト
go test -tags=integration ./...

# フロントエンド(npm ではなく pnpm を使用)
cd frontend
pnpm install
pnpm dev                    # 開発サーバー
pnpm build                  # 本番ビルド
```

## コントリビューション

コントリビューションを歓迎します。プルリクエストを送る前に [`CLA.md`](CLA.md) をお読みいただき、[`DEV_GUIDE.md`](DEV_GUIDE.md) の PR チェックリストに従ってください。リリースプロセスは [`docs/RELEASE_PROCESS.md`](docs/RELEASE_PROCESS.md) を参照してください。

## ライセンス

LightBridge は [GNU Lesser General Public License v3.0](LICENSE) の下でライセンスされています。

## リンク

- [GitHub Releases](https://github.com/WilliamWang1721/LightBridge/releases)
- [LinuxDO](https://linux.do/) — フレンドリーな開発者コミュニティ
