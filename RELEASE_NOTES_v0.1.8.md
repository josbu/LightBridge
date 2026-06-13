# Release v0.1.8 - LightBridge Connect

## 🎉 主要新功能

### LightBridge Connect - New API 深度集成

LightBridge 现在支持与 New API 实例的深度集成！通过 **LightBridge Connect**，您可以：

- ✅ **实时余额监控** - 自动同步 New API 账号余额（每 5 分钟）
- ✅ **智能警报系统** - 余额不足时多渠道通知（Dashboard/Email/Webhook）
- ✅ **自动保护机制** - 余额耗尽时自动禁用账号，避免服务中断
- ✅ **完整审计日志** - 所有余额变化和警报记录可追溯
- ✅ **安全令牌管理** - AES-256-GCM 加密存储系统访问令牌

### 使用方式

1. 创建 Custom Provider 账号时，选择 **New API** 预设
2. 输入您的 **系统访问令牌**（在 New API: 个人设置 → 安全设置中生成）
3. 点击"**验证令牌**"按钮验证连接
4. 配置**余额警报**：
   - 设置警报阈值（例如：100 元）
   - 选择通知渠道（Dashboard/Email/Webhook）
   - 启用"余额不足自动禁用"功能
5. 保存账号 → 系统自动开始监控

### 技术亮点

- 🔐 **安全第一**：系统访问令牌采用 AES-256-GCM 加密存储
- ⚡ **高性能**：优化的数据库索引，毫秒级查询
- 🌐 **国际化**：完整的中英文支持
- 📊 **可观测性**：3 个新数据库表记录所有操作历史
- 🔄 **自动同步**：后台服务定期同步，无需手动干预

## 📦 新增 API 端点

```
POST /api/v1/admin/accounts/:id/lightbridge-connect/verify-token
GET  /api/v1/admin/accounts/:id/lightbridge-connect/quota
POST /api/v1/admin/accounts/:id/lightbridge-connect/sync-quota
PUT  /api/v1/admin/accounts/:id/lightbridge-connect/alert-config
```

## 🗄️ 数据库变更

**新增迁移**: `151_lightbridge_connect.sql`

- ✅ `accounts.lightbridge_connect` - JSONB 字段存储配置
- ✅ `lightbridge_connect_quota_logs` - 余额变化审计日志
- ✅ `lightbridge_connect_alerts` - 警报历史记录

**⚠️ 升级须知**：需要运行数据库迁移
```bash
psql -d lightbridge -f backend/migrations/151_lightbridge_connect.sql
```

## 🎨 前端改进

- ✅ 新增 `LightBridgeConnectConfig.vue` 配置组件
- ✅ `CreateAccountModal` 支持 LightBridge Connect 配置
- ✅ Custom Provider 预设识别 New API
- ✅ 完整的类型定义和 API 客户端

## 🔧 后端改进

- ✅ `LightBridgeConnectService` - 核心业务逻辑
- ✅ `LightBridgeConnectSyncService` - 自动同步服务
- ✅ `LightBridgeConnectHandler` - HTTP API 处理器
- ✅ Wire 依赖注入完整配置

## 📚 文档

- 📖 完整的部署指南
- 📖 使用手册
- 📖 故障排查指南
- 📖 集成测试脚本

查看详细文档：`.claude-lightbridge-connect-complete.md`

## 🐛 Bug 修复

无重大 Bug 修复（新功能发布）

## ⚠️ 破坏性变更

无破坏性变更，完全向后兼容

## 📊 统计数据

- **新增代码**: ~3000 行
- **新增文件**: 15 个
- **新增 API**: 4 个端点
- **新增数据库表**: 3 个
- **开发时间**: 8 小时

## 🚀 升级步骤

### 1. 备份数据库
```bash
pg_dump lightbridge > lightbridge_backup_$(date +%Y%m%d).sql
```

### 2. 停止服务
```bash
systemctl stop lightbridge
# 或
pkill lightbridge
```

### 3. 更新代码
```bash
git fetch --tags
git checkout v0.1.8
```

### 4. 运行迁移
```bash
cd backend
psql -U lightbridge -d lightbridge -f migrations/151_lightbridge_connect.sql
```

### 5. 重新编译
```bash
# 后端
cd backend
go build -o lightbridge ./cmd/server

# 前端
cd frontend
npm install
npm run build
```

### 6. 重启服务
```bash
systemctl start lightbridge
# 或
./lightbridge
```

### 7. 验证部署
```bash
# 检查服务日志
tail -f lightbridge.log | grep "LightBridge Connect"

# 应该看到类似输出：
# [LightBridge Connect] Sync service started (interval: 5m0s)
```

## 🔍 测试验证

运行集成测试：
```bash
chmod +x .claude-test-lightbridge-connect.sh
export NEW_API_INSTANCE="http://your-new-api-instance:3000"
export NEW_API_TOKEN="sk-your-system-token"
export ADMIN_TOKEN="your-admin-token"
./.claude-test-lightbridge-connect.sh
```

## 🙏 致谢

感谢所有使用 LightBridge 的用户！

如有任何问题或建议，请在 GitHub Issues 中反馈。

---

**完整变更日志**: [v0.1.7...v0.1.8](https://github.com/Wei-Shaw/LightBridge/compare/v0.1.7...v0.1.8)

**下载地址**: [GitHub Releases](https://github.com/Wei-Shaw/LightBridge/releases/tag/v0.1.8)
