import { describe, expect, it } from 'vitest'
import { previewCoreRuntimeValue } from '@/utils/coreRuntimePreview'

describe('server-provided core path preview', () => {
  it.each([
    ['/opt/router app', '/opt/router app/data/sing-box/config.json'],
    ['D:\\Router App\\', 'D:\\Router App\\/data/sing-box/config.json'],
    ['', '/data/sing-box/config.json'],
    ['/srv/$CORE_BASE_PATH', '/srv/$CORE_BASE_PATH/data/sing-box/config.json'],
  ])('uses the exact backend substitution for %s', (base, expected) => {
    const paths = { appBasePath: base, coreBasePath: 'data/sing-box' }
    expect(previewCoreRuntimeValue('$APP_BASE_PATH/$CORE_BASE_PATH/config.json', paths)).toBe(
      expected,
    )
    expect(previewCoreRuntimeValue('prefix=$APP_BASE_PATH;$APP_BASE_PATH;$OTHER', paths)).toBe(
      `prefix=${base};${base};$OTHER`,
    )
  })
})
