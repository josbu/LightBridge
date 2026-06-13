-- 新增 Custom Provider 并迁移存量非官方 base_url 账户。
--
-- 背景：Custom provider 允许用户配置自定义上游端点(base_url) + 协议类型(protocol)。
-- 为清晰区分官方/自定义上游,本迁移:
-- 1) 把带非官方 base_url 的 openai/anthropic apikey 账户迁移为 platform='custom'
-- 2) 将其 base_url 保留,protocol 根据原平台推导
-- 3) 保留原分组成员关系(依赖"custom 不受分组类型限制"继续在原分组被调度,零破坏)
-- 4) 更新 user_platform_quotas 表的 CHECK 约束以允许 'custom' 平台
--
-- 兼容性：
-- - OpenAI/Anthropic 官方账户的 base_url 从此被忽略(GetBaseURL 强制返回官方 URL)
-- - Gemini base_url 保持可编辑(不锁定)
-- - Custom 账户可加入任意分组,按 protocol 与请求的入站 endpoint 匹配调度
--
-- 幂等：可重复执行——已是 custom 的账户不会被重复迁移;约束已存在则跳过。
--
-- 运维提示：部署后建议触发调度快照重建,使 custom 账户立即生效。

-- 官方 Base URL 常量(用于判定是否为非官方)
-- Anthropic: api.anthropic.com
-- OpenAI: api.openai.com

-- 1) 更新 user_platform_quotas 表的 platform CHECK 约束
-- 先删除旧约束,再添加包含 'custom' 的新约束
ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;
ALTER TABLE user_platform_quotas ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'custom'));

-- 2) 迁移 Anthropic apikey 账户(带非官方 base_url)
UPDATE accounts
SET platform = 'custom',
    extra = jsonb_set(
        COALESCE(extra, '{}'::jsonb),
        '{protocol}',
        '"anthropic_messages"'::jsonb
    ),
    updated_at = NOW()
WHERE platform = 'anthropic'
  AND type = 'apikey'
  AND credentials ? 'base_url'
  AND credentials->>'base_url' IS NOT NULL
  AND credentials->>'base_url' != ''
  AND credentials->>'base_url' NOT LIKE '%api.anthropic.com%';

-- 3) 迁移 OpenAI apikey 账户(带非官方 base_url)
-- 默认 protocol 为 openai_responses,如果有 chat/embeddings capability 则相应调整
UPDATE accounts
SET platform = 'custom',
    extra = jsonb_set(
        COALESCE(extra, '{}'::jsonb),
        '{protocol}',
        CASE
            -- 优先级: embeddings > chat > responses (按特异性排序)
            WHEN credentials->>'openai_capabilities' LIKE '%embeddings%' THEN '"openai_embeddings"'::jsonb
            WHEN credentials->>'openai_capabilities' LIKE '%chat%' THEN '"openai_chat_completions"'::jsonb
            ELSE '"openai_responses"'::jsonb
        END
    ),
    updated_at = NOW()
WHERE platform = 'openai'
  AND type = 'apikey'
  AND credentials ? 'base_url'
  AND credentials->>'base_url' IS NOT NULL
  AND credentials->>'base_url' != ''
  AND credentials->>'base_url' NOT LIKE '%api.openai.com%';
