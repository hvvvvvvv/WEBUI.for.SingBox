// @vitest-environment jsdom
import { createApp, h, nextTick, reactive } from 'vue'
import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { describe, expect, it, vi } from 'vitest'
import { ProfileSchema } from '@gen/profile/v1/profile_pb'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/lang', () => ({ default: { global: { t: (key: string) => key } } }))
vi.mock('@/hooks', async () => await import('@/hooks/useBool'))
import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'
import GeneralConfig from '@/views/ProfilesView/components/GeneralConfig.vue'

describe('general configuration', () => {
  it.each(['global', 'rule', 'direct'] as const)(
    'preserves default %s through the protobuf wire format',
    (mode) => {
      const profile = protoProfileToIProfile(undefined)
      expect(profile.experimental.default_mode).toBe('rule')
      profile.experimental.default_mode = mode
      const loaded = protoProfileToIProfile(
        fromBinary(
          ProfileSchema,
          toBinary(ProfileSchema, create(ProfileSchema, iProfileToProto(profile))),
        ),
      )
      expect(loaded.experimental.default_mode).toBe(mode)
      expect(protoProfileToIProfile(create(ProfileSchema, {})).experimental.default_mode).toBe(
        'rule',
      )
    },
  )

  it('edits default mode while showing the real managed cache location without a false path editor', async () => {
    const model = reactive(protoProfileToIProfile(undefined))
    model.experimental.cache_file.path = '/old/custom.db'
    const app = createApp({ render: () => h(GeneralConfig, { modelValue: model }) })
    app.component('Radio', {
      props: ['modelValue', 'options'],
      emits: ['update:modelValue'],
      setup:
        (props, { emit }) =>
        () =>
          h(
            'select',
            {
              value: props.modelValue,
              onChange: (e: Event) =>
                emit('update:modelValue', (e.target as HTMLSelectElement).value),
            },
            props.options.map((option: any) => h('option', { value: option.value }, option.label)),
          ),
    })
    app.component('Input', {
      props: ['modelValue'],
      setup: (props) => () => h('input', { value: props.modelValue }),
    })
    app.component('Switch', { render: () => h('span') })
    for (const name of ['Divider', 'Button'])
      app.component(name, {
        setup:
          (_, { slots }) =>
          () =>
            h('div', slots.default?.()),
      })
    const host = document.createElement('div')
    app.mount(host)
    try {
      const mode = host.querySelector('select')!
      expect(Array.from(mode.options).map((option) => option.value)).toEqual([
        'global',
        'rule',
        'direct',
      ])
      mode.value = 'global'
      mode.dispatchEvent(new Event('change', { bubbles: true }))
      await nextTick()
      expect(model.experimental.default_mode).toBe('global')
      expect(host.textContent).toContain('cache.db')
      expect(host.textContent).toContain('kernel.cache_file.managedPath')
      expect(
        Array.from(host.querySelectorAll('input')).some(
          (input) => input.value === '/old/custom.db',
        ),
      ).toBe(false)
      expect(model.experimental.cache_file.path).toBe('/old/custom.db')
    } finally {
      app.unmount()
    }
  })
})
