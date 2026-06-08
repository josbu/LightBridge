import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ImportDataModal from '@/components/admin/account/ImportDataModal.vue'
import { adminAPI } from '@/api/admin'

const showError = vi.fn()
const showSuccess = vi.fn()

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      importData: vi.fn()
    },
    groups: {
      getByPlatform: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

const textEncoder = new TextEncoder()

const crc32Table = Array.from({ length: 256 }, (_, index) => {
  let crc = index
  for (let bit = 0; bit < 8; bit++) {
    crc = (crc & 1) ? (0xedb88320 ^ (crc >>> 1)) : (crc >>> 1)
  }
  return crc >>> 0
})

const crc32 = (bytes: Uint8Array) => {
  let crc = 0xffffffff
  for (const byte of bytes) {
    crc = (crc >>> 8) ^ crc32Table[(crc ^ byte) & 0xff]
  }
  return (crc ^ 0xffffffff) >>> 0
}

const writeUint16 = (target: number[], value: number) => {
  target.push(value & 0xff, (value >>> 8) & 0xff)
}

const writeUint32 = (target: number[], value: number) => {
  target.push(value & 0xff, (value >>> 8) & 0xff, (value >>> 16) & 0xff, (value >>> 24) & 0xff)
}

const makeStoredZip = (entries: Array<{ name: string; content: string }>) => {
  const output: number[] = []
  const centralDirectory: number[] = []
  for (const entry of entries) {
    const name = textEncoder.encode(entry.name)
    const content = textEncoder.encode(entry.content)
    const checksum = crc32(content)
    const localHeaderOffset = output.length

    writeUint32(output, 0x04034b50)
    writeUint16(output, 20)
    writeUint16(output, 0)
    writeUint16(output, 0)
    writeUint16(output, 0)
    writeUint16(output, 0)
    writeUint32(output, checksum)
    writeUint32(output, content.length)
    writeUint32(output, content.length)
    writeUint16(output, name.length)
    writeUint16(output, 0)
    output.push(...name, ...content)

    writeUint32(centralDirectory, 0x02014b50)
    writeUint16(centralDirectory, 20)
    writeUint16(centralDirectory, 20)
    writeUint16(centralDirectory, 0)
    writeUint16(centralDirectory, 0)
    writeUint16(centralDirectory, 0)
    writeUint16(centralDirectory, 0)
    writeUint32(centralDirectory, checksum)
    writeUint32(centralDirectory, content.length)
    writeUint32(centralDirectory, content.length)
    writeUint16(centralDirectory, name.length)
    writeUint16(centralDirectory, 0)
    writeUint16(centralDirectory, 0)
    writeUint16(centralDirectory, 0)
    writeUint16(centralDirectory, 0)
    writeUint32(centralDirectory, 0)
    writeUint32(centralDirectory, localHeaderOffset)
    centralDirectory.push(...name)
  }

  const centralDirectoryOffset = output.length
  output.push(...centralDirectory)
  writeUint32(output, 0x06054b50)
  writeUint16(output, 0)
  writeUint16(output, 0)
  writeUint16(output, entries.length)
  writeUint16(output, entries.length)
  writeUint32(output, centralDirectory.length)
  writeUint32(output, centralDirectoryOffset)
  writeUint16(output, 0)
  return new Uint8Array(output)
}

describe('ImportDataModal', () => {
  beforeEach(() => {
    showError.mockReset()
    showSuccess.mockReset()
    vi.mocked(adminAPI.accounts.importData).mockReset()
    vi.mocked(adminAPI.groups.getByPlatform).mockResolvedValue([])
  })

  it('未选择文件时提示错误', async () => {
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    await wrapper.find('form').trigger('submit')
    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportSelectFile')
  })

  it('无效 JSON 时提示解析失败', async () => {
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File(['invalid json'], 'data.json', { type: 'application/json' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('invalid json')
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.accounts.dataImportParseFailed')
  })

  it('兼容性模式允许 TXT 原文导入', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const file = new File(['refresh_token=rt'], 'tokens.txt', { type: 'text/plain' })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('refresh_token=rt')
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    await checkboxes[checkboxes.length - 1].setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: 'refresh_token=rt',
      skip_default_group_bind: true,
      compatibility_mode: true,
      group_ids: [],
      account_defaults: undefined
    })
  })

  it('支持一次选择多个 JSON 文件并逐个导入', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const input = wrapper.find('input[type="file"]')
    const first = new File(['{"type":"LightBridge-data","version":1,"proxies":[],"accounts":[]}'], 'first.json', {
      type: 'application/json'
    })
    const second = new File(['{"type":"LightBridge-data","version":1,"proxies":[],"accounts":[]}'], 'second.json', {
      type: 'application/json'
    })
    Object.defineProperty(first, 'text', {
      value: () => Promise.resolve('{"type":"LightBridge-data","version":1,"proxies":[],"accounts":[]}')
    })
    Object.defineProperty(second, 'text', {
      value: () => Promise.resolve('{"type":"LightBridge-data","version":1,"proxies":[],"accounts":[]}')
    })
    Object.defineProperty(input.element, 'files', {
      value: [first, second]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledTimes(2)
    expect(adminAPI.accounts.importData).toHaveBeenNthCalledWith(1, {
      data: { type: 'LightBridge-data', version: 1, proxies: [], accounts: [] },
      skip_default_group_bind: true,
      compatibility_mode: false,
      group_ids: [],
      account_defaults: undefined
    })
    expect(adminAPI.accounts.importData).toHaveBeenNthCalledWith(2, {
      data: { type: 'LightBridge-data', version: 1, proxies: [], accounts: [] },
      skip_default_group_bind: true,
      compatibility_mode: false,
      group_ids: [],
      account_defaults: undefined
    })
  })

  it('导入时支持选择分组并批量覆盖账号设置', async () => {
    vi.mocked(adminAPI.groups.getByPlatform).mockResolvedValue([
      { id: 10, name: 'Group A', platform: 'openai', status: 'active', sort_order: 1 },
      { id: 11, name: 'Group B', platform: 'openai', status: 'active', sort_order: 2 }
    ] as any)
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })
    await flushPromises()

    await wrapper.find('select').setValue(['10', '11'])
    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    await checkboxes[0].setValue(true)
    const numberInputs = wrapper.findAll('input[type="number"]')
    await numberInputs[0].setValue(24)
    await numberInputs[1].setValue(7)
    await numberInputs[2].setValue(0.5)
    await checkboxes[1].setValue(false)

    const input = wrapper.find('input[type="file"]')
    const file = new File(['{"type":"LightBridge-data","version":1,"proxies":[],"accounts":[]}'], 'data.json', {
      type: 'application/json'
    })
    Object.defineProperty(file, 'text', {
      value: () => Promise.resolve('{"type":"LightBridge-data","version":1,"proxies":[],"accounts":[]}')
    })
    Object.defineProperty(input.element, 'files', {
      value: [file]
    })

    await input.trigger('change')
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      data: { type: 'LightBridge-data', version: 1, proxies: [], accounts: [] },
      skip_default_group_bind: false,
      compatibility_mode: false,
      group_ids: [10, 11],
      account_defaults: {
        concurrency: 24,
        priority: 7,
        rate_multiplier: 0.5,
        auto_pause_on_expired: false
      }
    })
  })

  it('选择 ZIP 后自动解压并只导入 JSON/TXT 文件', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 1,
      account_failed: 0
    })
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const zipBytes = makeStoredZip([
      { name: 'accounts/first.json', content: '{"type":"LightBridge-data","version":1,"proxies":[],"accounts":[]}' },
      { name: 'tokens.txt', content: 'refresh_token=rt' },
      { name: 'notes.md', content: '# ignored' }
    ])
    const zipFile = new File([zipBytes], 'bundle.zip', { type: 'application/zip' })
    Object.defineProperty(zipFile, 'arrayBuffer', {
      value: () => Promise.resolve(zipBytes.buffer)
    })

    const input = wrapper.find('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      value: [zipFile]
    })

    await input.trigger('change')
    await flushPromises()
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.accounts.dataImportSelectedFiles')
    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    await checkboxes[checkboxes.length - 1].setValue(true)
    await wrapper.find('form').trigger('submit')

    await vi.waitFor(() => {
      expect(adminAPI.accounts.importData).toHaveBeenCalledTimes(2)
    })
    expect(adminAPI.accounts.importData).toHaveBeenNthCalledWith(1, {
      data: { type: 'LightBridge-data', version: 1, proxies: [], accounts: [] },
      skip_default_group_bind: true,
      compatibility_mode: true,
      group_ids: [],
      account_defaults: undefined
    })
    expect(adminAPI.accounts.importData).toHaveBeenNthCalledWith(2, {
      data: 'refresh_token=rt',
      skip_default_group_bind: true,
      compatibility_mode: true,
      group_ids: [],
      account_defaults: undefined
    })
  })

  it('支持粘贴下载链接由服务端导入', async () => {
    vi.mocked(adminAPI.accounts.importData).mockResolvedValue({
      proxy_created: 0,
      proxy_reused: 0,
      proxy_failed: 0,
      account_created: 2,
      account_failed: 0
    })
    const wrapper = mount(ImportDataModal, {
      props: { show: true },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' }
        }
      }
    })

    const urlInput = wrapper.find('input[type="url"]')
    await urlInput.setValue('https://example.com/accounts.zip')
    const checkboxes = wrapper.findAll('input[type="checkbox"]')
    await checkboxes[checkboxes.length - 1].setValue(true)
    await wrapper.find('form').trigger('submit')
    await flushPromises()

    expect(adminAPI.accounts.importData).toHaveBeenCalledWith({
      source_url: 'https://example.com/accounts.zip',
      skip_default_group_bind: true,
      compatibility_mode: true,
      group_ids: [],
      account_defaults: undefined
    })
  })
})
