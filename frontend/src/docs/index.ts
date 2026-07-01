import gettingStarted from './content/getting-started.md?raw'
import lightbridgeRouter from './content/lightbridge-router.md?raw'
import release029Preview1 from './content/release-0.2.9-preview.1.md?raw'
import release0210Preview from './content/release-0.2.10-preview.md?raw'
import release0213Preview from './content/release-0.2.13-preview.md?raw'

export interface LightBridgeDoc {
  id: string
  title: string
  group: string
  description?: string
  content: string
}

export const lightBridgeDocs: LightBridgeDoc[] = [
  {
    id: 'getting-started',
    title: '文档中心',
    group: '入门',
    description: 'LightBridge 模块与功能文档入口',
    content: gettingStarted,
  },
  {
    id: 'lightbridge-router',
    title: 'LightBridge Router',
    group: '核心能力',
    description: '全协议消息路由、透传模式与混合分组调度说明',
    content: lightbridgeRouter,
  },
  {
    id: 'release-0.2.13-preview',
    title: '0.2.13 版本更新',
    group: '版本更新',
    description: '路由错误诊断增强、应用内文档中心与前端重构',
    content: release0213Preview,
  },
  {
    id: 'release-0.2.10-preview',
    title: '0.2.10-preview 版本更新',
    group: '版本更新',
    description: '分组调度修复与预览版升级检测修复',
    content: release0210Preview,
  },
  {
    id: 'release-0.2.9-preview.1',
    title: '0.2.9-preview.1 版本更新',
    group: '版本更新',
    description: '全协议 Router 分组调度修复预览版',
    content: release029Preview1,
  },
]

export function findDocById(id: string | null | undefined): LightBridgeDoc {
  return lightBridgeDocs.find((doc) => doc.id === id) ?? lightBridgeDocs[0]
}
