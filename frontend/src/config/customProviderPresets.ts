/**
 * Custom Provider 预设配置
 *
 * 从 Cherry Studio 迁移的 Provider 预设列表，用于快速创建 Custom 账号。
 * 用户在添加 Custom 账号时可从预设列表选择，系统自动填充 Base URL 和协议类型。
 */

export interface CustomProviderPreset {
  /** 预设 ID（唯一标识） */
  id: string
  /** 显示名称 */
  name: string
  /** Base URL */
  baseUrl: string
  /**
   * 支持的协议类型
   * - openai-chat: OpenAI Chat Completions
   * - openai-responses: OpenAI Responses
   * - openai-embeddings: OpenAI Embeddings
   * - anthropic: Anthropic Messages
   * - gemini: Gemini
   */
  protocol: 'openai-chat' | 'openai-responses' | 'openai-embeddings' | 'anthropic' | 'gemini'
  /** 备注/说明 */
  description?: string
  /** 官网链接 */
  officialUrl?: string
  /** API Key 获取页面 */
  apiKeyUrl?: string
  /** 文档链接 */
  docsUrl?: string
  /** 是否支持 LightBridge Connect（深度集成） */
  supportsLightBridgeConnect?: boolean
  /** LightBridge Connect 类型 */
  lightBridgeConnectType?: 'new-api'
}

/**
 * Custom Provider 预设列表
 *
 * 按协议类型分组，优先展示常用的 OpenAI 兼容服务。
 */
export const CUSTOM_PROVIDER_PRESETS: CustomProviderPreset[] = [
  // === OpenAI Chat Completions 兼容服务 ===
  {
    id: 'silicon',
    name: 'Silicon Flow',
    baseUrl: 'https://api.siliconflow.cn/v1',
    protocol: 'openai-chat',
    description: '硅基流动 - 国内稳定的 OpenAI 兼容服务',
    officialUrl: 'https://www.siliconflow.cn',
    apiKeyUrl: 'https://cloud.siliconflow.cn/i/d1nTBKXU',
    docsUrl: 'https://docs.siliconflow.cn/'
  },
  {
    id: 'deepseek',
    name: 'DeepSeek',
    baseUrl: 'https://api.deepseek.com/v1',
    protocol: 'openai-chat',
    description: 'DeepSeek - 高性价比的 AI 服务',
    officialUrl: 'https://deepseek.com/',
    apiKeyUrl: 'https://platform.deepseek.com/api_keys',
    docsUrl: 'https://platform.deepseek.com/api-docs/'
  },
  {
    id: 'zhipu',
    name: '智谱 AI (ZhiPu)',
    baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
    protocol: 'openai-chat',
    description: '智谱 AI - GLM 系列模型',
    officialUrl: 'https://open.bigmodel.cn/',
    apiKeyUrl: 'https://open.bigmodel.cn/usercenter/apikeys',
    docsUrl: 'https://docs.bigmodel.cn/'
  },
  {
    id: 'moonshot',
    name: 'Moonshot AI (Kimi)',
    baseUrl: 'https://api.moonshot.cn/v1',
    protocol: 'openai-chat',
    description: 'Moonshot AI - Kimi 模型',
    officialUrl: 'https://www.moonshot.cn/',
    apiKeyUrl: 'https://platform.moonshot.cn/console/api-keys',
    docsUrl: 'https://platform.moonshot.cn/docs/'
  },
  {
    id: 'yi',
    name: '零一万物 (Yi)',
    baseUrl: 'https://api.lingyiwanwu.com/v1',
    protocol: 'openai-chat',
    description: '零一万物 - Yi 系列模型',
    officialUrl: 'https://platform.lingyiwanwu.com/',
    apiKeyUrl: 'https://platform.lingyiwanwu.com/apikeys',
    docsUrl: 'https://platform.lingyiwanwu.com/docs'
  },
  {
    id: 'minimax',
    name: 'MiniMax',
    baseUrl: 'https://api.minimaxi.com/v1',
    protocol: 'openai-chat',
    description: 'MiniMax - 面壁智能',
    officialUrl: 'https://platform.minimaxi.com/',
    apiKeyUrl: 'https://platform.minimaxi.com/user-center/basic-information/interface-key',
    docsUrl: 'https://platform.minimaxi.com/docs/api-reference/text-openai-api'
  },
  {
    id: 'baichuan',
    name: '百川智能 (BAICHUAN)',
    baseUrl: 'https://api.baichuan-ai.com/v1',
    protocol: 'openai-chat',
    description: '百川智能 - 百川系列模型',
    officialUrl: 'https://www.baichuan-ai.com/',
    apiKeyUrl: 'https://platform.baichuan-ai.com/console/apikey',
    docsUrl: 'https://platform.baichuan-ai.com/docs'
  },
  {
    id: 'dashscope',
    name: '阿里百炼 (Bailian)',
    baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    protocol: 'openai-chat',
    description: '阿里云百炼 - 通义系列模型',
    officialUrl: 'https://www.aliyun.com/product/bailian',
    apiKeyUrl: 'https://bailian.console.aliyun.com/?tab=model#/api-key',
    docsUrl: 'https://help.aliyun.com/zh/model-studio/getting-started/'
  },
  {
    id: 'stepfun',
    name: '阶跃星辰 (StepFun)',
    baseUrl: 'https://api.stepfun.com/v1',
    protocol: 'openai-chat',
    description: '阶跃星辰 - Step 系列模型',
    officialUrl: 'https://platform.stepfun.com/',
    apiKeyUrl: 'https://platform.stepfun.com/interface-key',
    docsUrl: 'https://platform.stepfun.com/docs/overview/concept'
  },
  {
    id: 'doubao',
    name: '豆包 (Doubao)',
    baseUrl: 'https://ark.cn-beijing.volces.com/api/v3',
    protocol: 'openai-chat',
    description: '字节跳动豆包 - 火山引擎',
    officialUrl: 'https://console.volcengine.com/ark/',
    apiKeyUrl: 'https://www.volcengine.com/experience/ark',
    docsUrl: 'https://www.volcengine.com/docs/82379/1182403'
  },
  {
    id: 'hunyuan',
    name: '腾讯混元 (Hunyuan)',
    baseUrl: 'https://api.hunyuan.cloud.tencent.com/v1',
    protocol: 'openai-chat',
    description: '腾讯混元大模型',
    officialUrl: 'https://cloud.tencent.com/product/hunyuan',
    apiKeyUrl: 'https://console.cloud.tencent.com/hunyuan/api-key',
    docsUrl: 'https://cloud.tencent.com/document/product/1729/111007'
  },
  {
    id: 'infini',
    name: '无问芯穹 (Infini)',
    baseUrl: 'https://cloud.infini-ai.com/maas/v1',
    protocol: 'openai-chat',
    description: '无问芯穹 AI 云平台',
    officialUrl: 'https://cloud.infini-ai.com/',
    apiKeyUrl: 'https://cloud.infini-ai.com/iam/secret/key',
    docsUrl: 'https://docs.infini-ai.com/gen-studio/api/maas.html'
  },
  {
    id: 'mimo',
    name: '小米 MiMo',
    baseUrl: 'https://api.xiaomimimo.com/v1',
    protocol: 'openai-chat',
    description: '小米 MiMo AI 平台',
    officialUrl: 'https://platform.xiaomimimo.com/',
    apiKeyUrl: 'https://platform.xiaomimimo.com/#/console/usage',
    docsUrl: 'https://platform.xiaomimimo.com/#/docs/welcome'
  },

  // === 国际 OpenAI 兼容服务 ===
  {
    id: 'groq',
    name: 'Groq',
    baseUrl: 'https://api.groq.com/openai/v1',
    protocol: 'openai-chat',
    description: 'Groq - 超快推理速度',
    officialUrl: 'https://groq.com/',
    apiKeyUrl: 'https://console.groq.com/keys',
    docsUrl: 'https://console.groq.com/docs/quickstart'
  },
  {
    id: 'together',
    name: 'Together AI',
    baseUrl: 'https://api.together.xyz/v1',
    protocol: 'openai-chat',
    description: 'Together AI - 开源模型平台',
    officialUrl: 'https://www.together.ai/',
    apiKeyUrl: 'https://api.together.ai/settings/api-keys',
    docsUrl: 'https://docs.together.ai/docs/introduction'
  },
  {
    id: 'fireworks',
    name: 'Fireworks AI',
    baseUrl: 'https://api.fireworks.ai/inference/v1',
    protocol: 'openai-chat',
    description: 'Fireworks AI - 快速推理',
    officialUrl: 'https://fireworks.ai/',
    apiKeyUrl: 'https://fireworks.ai/account/api-keys',
    docsUrl: 'https://docs.fireworks.ai/getting-started/introduction'
  },
  {
    id: 'hyperbolic',
    name: 'Hyperbolic',
    baseUrl: 'https://api.hyperbolic.xyz/v1',
    protocol: 'openai-chat',
    description: 'Hyperbolic - 去中心化 AI',
    officialUrl: 'https://app.hyperbolic.xyz',
    apiKeyUrl: 'https://app.hyperbolic.xyz/settings',
    docsUrl: 'https://docs.hyperbolic.xyz'
  },
  {
    id: 'nvidia',
    name: 'NVIDIA NIM',
    baseUrl: 'https://integrate.api.nvidia.com/v1',
    protocol: 'openai-chat',
    description: 'NVIDIA 推理微服务',
    officialUrl: 'https://build.nvidia.com/explore/discover',
    apiKeyUrl: 'https://build.nvidia.com/meta/llama-3_1-405b-instruct',
    docsUrl: 'https://docs.api.nvidia.com/nim/reference/llm-apis'
  },
  {
    id: 'cerebras',
    name: 'Cerebras AI',
    baseUrl: 'https://api.cerebras.ai/v1',
    protocol: 'openai-chat',
    description: 'Cerebras - 超大芯片推理',
    officialUrl: 'https://www.cerebras.ai',
    apiKeyUrl: 'https://cloud.cerebras.ai',
    docsUrl: 'https://inference-docs.cerebras.ai/introduction'
  },
  {
    id: 'mistral',
    name: 'Mistral AI',
    baseUrl: 'https://api.mistral.ai/v1',
    protocol: 'openai-chat',
    description: 'Mistral AI - 欧洲领先',
    officialUrl: 'https://mistral.ai',
    apiKeyUrl: 'https://console.mistral.ai/api-keys/',
    docsUrl: 'https://docs.mistral.ai'
  },
  {
    id: 'perplexity',
    name: 'Perplexity',
    baseUrl: 'https://api.perplexity.ai/v1',
    protocol: 'openai-chat',
    description: 'Perplexity - 搜索增强',
    officialUrl: 'https://perplexity.ai/',
    apiKeyUrl: 'https://www.perplexity.ai/settings/api',
    docsUrl: 'https://docs.perplexity.ai/home'
  },
  {
    id: 'grok',
    name: 'Grok (X.AI)',
    baseUrl: 'https://api.x.ai/v1',
    protocol: 'openai-chat',
    description: 'Grok - X.AI 的大模型',
    officialUrl: 'https://x.ai/',
    apiKeyUrl: 'https://x.ai/',
    docsUrl: 'https://docs.x.ai/'
  },

  // === 聚合服务 ===
  {
    id: 'openrouter',
    name: 'OpenRouter',
    baseUrl: 'https://openrouter.ai/api/v1',
    protocol: 'openai-chat',
    description: 'OpenRouter - 统一接口访问多模型',
    officialUrl: 'https://openrouter.ai/',
    apiKeyUrl: 'https://openrouter.ai/settings/keys',
    docsUrl: 'https://openrouter.ai/docs/quick-start'
  },
  {
    id: '302ai',
    name: '302.AI',
    baseUrl: 'https://api.302.ai/v1',
    protocol: 'openai-chat',
    description: '302.AI - AI 模型聚合',
    officialUrl: 'https://302.ai',
    apiKeyUrl: 'https://dash.302.ai/apis/list',
    docsUrl: 'https://302ai.apifox.cn/api-147522039'
  },
  {
    id: 'aihubmix',
    name: 'AiHubMix',
    baseUrl: 'https://aihubmix.com/v1',
    protocol: 'openai-chat',
    description: 'AiHubMix - 多模型整合',
    officialUrl: 'https://aihubmix.com',
    apiKeyUrl: 'https://aihubmix.com',
    docsUrl: 'https://doc.aihubmix.com/'
  },
  {
    id: 'ocoolai',
    name: 'ocoolAI',
    baseUrl: 'https://api.ocoolai.com/v1',
    protocol: 'openai-chat',
    description: 'ocoolAI - 模型聚合服务',
    officialUrl: 'https://one.ocoolai.com/',
    apiKeyUrl: 'https://one.ocoolai.com/token',
    docsUrl: 'https://docs.ocoolai.com/'
  },

  // === 本地部署 ===
  {
    id: 'ollama',
    name: 'Ollama',
    baseUrl: 'http://localhost:11434/v1',
    protocol: 'openai-chat',
    description: 'Ollama - 本地大模型运行',
    officialUrl: 'https://ollama.com/',
    docsUrl: 'https://github.com/ollama/ollama/tree/main/docs'
  },
  {
    id: 'lmstudio',
    name: 'LM Studio',
    baseUrl: 'http://localhost:1234/v1',
    protocol: 'openai-chat',
    description: 'LM Studio - 本地模型托管',
    officialUrl: 'https://lmstudio.ai/',
    docsUrl: 'https://lmstudio.ai/docs'
  },
  {
    id: 'new-api',
    name: 'New API',
    baseUrl: 'http://localhost:3000/v1',
    protocol: 'openai-chat',
    description: 'New API - 自建中转服务',
    officialUrl: 'https://docs.newapi.pro/',
    docsUrl: 'https://docs.newapi.pro',
    supportsLightBridgeConnect: true,
    lightBridgeConnectType: 'new-api'
  },
  {
    id: 'gpustack',
    name: 'GPUStack',
    baseUrl: 'http://localhost:8080/v1-openai',
    protocol: 'openai-chat',
    description: 'GPUStack - GPU 集群管理',
    officialUrl: 'https://gpustack.ai/',
    docsUrl: 'https://docs.gpustack.ai/latest/'
  },

  // === OpenAI Responses 协议（官方 API） ===
  {
    id: 'openai-responses',
    name: 'OpenAI (Responses API)',
    baseUrl: 'https://api.openai.com',
    protocol: 'openai-responses',
    description: 'OpenAI 官方 Responses API',
    officialUrl: 'https://openai.com/',
    apiKeyUrl: 'https://platform.openai.com/api-keys',
    docsUrl: 'https://platform.openai.com/docs'
  },
  {
    id: 'huggingface',
    name: 'Hugging Face',
    baseUrl: 'https://router.huggingface.co/v1',
    protocol: 'openai-responses',
    description: 'Hugging Face 推理路由',
    officialUrl: 'https://huggingface.co/',
    apiKeyUrl: 'https://huggingface.co/settings/tokens',
    docsUrl: 'https://huggingface.co/docs'
  },

  // === OpenAI Embeddings ===
  {
    id: 'jina',
    name: 'Jina AI',
    baseUrl: 'https://api.jina.ai/v1',
    protocol: 'openai-embeddings',
    description: 'Jina AI - Embedding 专用',
    officialUrl: 'https://jina.ai',
    apiKeyUrl: 'https://jina.ai/',
    docsUrl: 'https://jina.ai'
  },
  {
    id: 'voyageai',
    name: 'Voyage AI',
    baseUrl: 'https://api.voyageai.com/v1',
    protocol: 'openai-embeddings',
    description: 'Voyage AI - Embedding 服务',
    officialUrl: 'https://www.voyageai.com/',
    apiKeyUrl: 'https://dashboard.voyageai.com/organization/api-keys',
    docsUrl: 'https://docs.voyageai.com/docs'
  },

  // === Anthropic Messages ===
  {
    id: 'zhipu-anthropic',
    name: '智谱 AI (Anthropic 协议)',
    baseUrl: 'https://open.bigmodel.cn/api/anthropic',
    protocol: 'anthropic',
    description: '智谱 AI - 兼容 Anthropic 协议',
    officialUrl: 'https://open.bigmodel.cn/',
    apiKeyUrl: 'https://open.bigmodel.cn/usercenter/apikeys',
    docsUrl: 'https://docs.bigmodel.cn/'
  },
  {
    id: 'deepseek-anthropic',
    name: 'DeepSeek (Anthropic 协议)',
    baseUrl: 'https://api.deepseek.com/anthropic',
    protocol: 'anthropic',
    description: 'DeepSeek - 兼容 Anthropic 协议',
    officialUrl: 'https://deepseek.com/',
    apiKeyUrl: 'https://platform.deepseek.com/api_keys',
    docsUrl: 'https://platform.deepseek.com/api-docs/'
  },
  {
    id: 'moonshot-anthropic',
    name: 'Moonshot AI (Anthropic 协议)',
    baseUrl: 'https://api.moonshot.cn/anthropic',
    protocol: 'anthropic',
    description: 'Moonshot AI - 兼容 Anthropic 协议',
    officialUrl: 'https://www.moonshot.cn/',
    apiKeyUrl: 'https://platform.moonshot.cn/console/api-keys',
    docsUrl: 'https://platform.moonshot.cn/docs/'
  },
  {
    id: 'minimax-anthropic',
    name: 'MiniMax (Anthropic 协议)',
    baseUrl: 'https://api.minimaxi.com/anthropic',
    protocol: 'anthropic',
    description: 'MiniMax - 兼容 Anthropic 协议',
    officialUrl: 'https://platform.minimaxi.com/',
    apiKeyUrl: 'https://platform.minimaxi.com/user-center/basic-information/interface-key',
    docsUrl: 'https://platform.minimaxi.com/docs/api-reference/text-openai-api'
  },
  {
    id: 'dashscope-anthropic',
    name: '阿里百炼 (Anthropic 协议)',
    baseUrl: 'https://dashscope.aliyuncs.com/apps/anthropic',
    protocol: 'anthropic',
    description: '阿里云百炼 - 兼容 Anthropic 协议',
    officialUrl: 'https://www.aliyun.com/product/bailian',
    apiKeyUrl: 'https://bailian.console.aliyun.com/?tab=model#/api-key',
    docsUrl: 'https://help.aliyun.com/zh/model-studio/getting-started/'
  },
  {
    id: 'longcat-anthropic',
    name: 'LongCat (Anthropic 协议)',
    baseUrl: 'https://api.longcat.chat/anthropic',
    protocol: 'anthropic',
    description: 'LongCat - 兼容 Anthropic 协议',
    officialUrl: 'https://longcat.chat',
    apiKeyUrl: 'https://longcat.chat/platform/api_keys',
    docsUrl: 'https://longcat.chat/platform/docs/zh/'
  },
  {
    id: 'mimo-anthropic',
    name: '小米 MiMo (Anthropic 协议)',
    baseUrl: 'https://api.xiaomimimo.com/anthropic',
    protocol: 'anthropic',
    description: '小米 MiMo - 兼容 Anthropic 协议',
    officialUrl: 'https://platform.xiaomimimo.com/',
    apiKeyUrl: 'https://platform.xiaomimimo.com/#/console/usage',
    docsUrl: 'https://platform.xiaomimimo.com/#/docs/welcome'
  }
]

/**
 * 按协议类型分组预设
 */
export const PRESETS_BY_PROTOCOL = {
  'openai-chat': CUSTOM_PROVIDER_PRESETS.filter(p => p.protocol === 'openai-chat'),
  'openai-responses': CUSTOM_PROVIDER_PRESETS.filter(p => p.protocol === 'openai-responses'),
  'openai-embeddings': CUSTOM_PROVIDER_PRESETS.filter(p => p.protocol === 'openai-embeddings'),
  'anthropic': CUSTOM_PROVIDER_PRESETS.filter(p => p.protocol === 'anthropic'),
  'gemini': CUSTOM_PROVIDER_PRESETS.filter(p => p.protocol === 'gemini')
}

/**
 * 根据 ID 查找预设
 */
export function findPresetById(id: string): CustomProviderPreset | undefined {
  return CUSTOM_PROVIDER_PRESETS.find(p => p.id === id)
}

/**
 * 根据协议类型获取预设列表
 */
export function getPresetsByProtocol(protocol: CustomProviderPreset['protocol']): CustomProviderPreset[] {
  return PRESETS_BY_PROTOCOL[protocol] || []
}
