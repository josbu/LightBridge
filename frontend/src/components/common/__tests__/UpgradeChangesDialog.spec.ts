import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UpgradeChangesDialog from '../UpgradeChangesDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('UpgradeChangesDialog', () => {
  it('keeps the restart action in the dialog footer and emits restart', async () => {
    const wrapper = mount(UpgradeChangesDialog, {
      props: {
        show: true,
        version: '0.3.0',
        body: '# Changes\n\n' + 'Long release note. '.repeat(500)
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<section class="modal-content"><div class="modal-body"><slot /></div><footer class="modal-footer"><slot name="footer" /></footer></section>'
          }
        }
      }
    })

    const restartButton = wrapper.findAll('button').find((button) =>
      button.text().includes('version.restartNow')
    )

    expect(restartButton).toBeDefined()
    expect(restartButton!.element.closest('.modal-footer')).not.toBeNull()

    await restartButton!.trigger('click')
    expect(wrapper.emitted('restart')).toHaveLength(1)
  })
})
