import gettingStarted from './content/getting-started.md?raw'
import release029Preview1 from './content/release-0.2.9-preview.1.md?raw'

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
