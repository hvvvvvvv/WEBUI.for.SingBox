import { beforeEach, describe, expect, it, vi } from 'vitest'
import { KernelConfigService } from '../../../gen/kernel/v1/kernel_config_service_pb'
import { ProfileService } from '../../../gen/profile/v1/profile_service_pb'
import { Branch } from '@/enums/app'

const mocks = vi.hoisted(() => ({
  rpc: { generateConfig: vi.fn(), getProfile: vi.fn() },
  createRpcClient: vi.fn(),
  appConfig: { config: { branch: 'main' } },
  iProfileToProto: vi.fn(),
}))

vi.mock('@/bridge', () => ({ createRpcClient: mocks.createRpcClient }))
vi.mock('@/stores', () => ({ useAppConfigStore: () => mocks.appConfig }))
vi.mock('../profileRpc', () => ({ iProfileToProto: mocks.iProfileToProto }))

import { generateConfigViaRpc, generateConfigViaRpcByProfile } from '../generator'

const profile = { id: 'profile-id' } as IProfile
const convertedProfile = { id: 'converted-profile-id' }
const config = { outbounds: [{ type: 'direct' }] }

describe('configuration generation options', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    mocks.appConfig.config.branch = Branch.Main
    mocks.createRpcClient.mockReturnValue(mocks.rpc)
    mocks.iProfileToProto.mockReturnValue(convertedProfile)
    mocks.rpc.generateConfig.mockResolvedValue({ config })
    mocks.rpc.getProfile.mockResolvedValue({ profile })
  })

  it.each([
    { branch: Branch.Main, adaptation: false },
    { branch: Branch.Alpha, adaptation: true },
  ])('defaults Alpha adaptation on $branch to $adaptation', async ({ branch, adaptation }) => {
    mocks.appConfig.config.branch = branch

    await expect(generateConfigViaRpcByProfile(profile)).resolves.toEqual(config)

    expect(mocks.createRpcClient).toHaveBeenCalledWith(KernelConfigService)
    expect(mocks.iProfileToProto).toHaveBeenCalledWith(profile)
    expect(mocks.rpc.generateConfig).toHaveBeenCalledExactlyOnceWith({
      profile: convertedProfile,
      options: {
        enableAlphaConfigAdaptation: adaptation,
        enableMixinProcessing: true,
        enableScriptProcessing: true,
      },
    })
  })

  it.each([
    { branch: Branch.Main, adaptation: true },
    { branch: Branch.Main, adaptation: false },
    { branch: Branch.Alpha, adaptation: true },
    { branch: Branch.Alpha, adaptation: false },
  ])('honors explicit adaptation on $branch: $adaptation', async ({ branch, adaptation }) => {
    mocks.appConfig.config.branch = branch

    await generateConfigViaRpcByProfile(profile, { enableAlphaConfigAdaptation: adaptation })

    expect(mocks.rpc.generateConfig).toHaveBeenCalledExactlyOnceWith({
      profile: convertedProfile,
      options: {
        enableAlphaConfigAdaptation: adaptation,
        enableMixinProcessing: true,
        enableScriptProcessing: true,
      },
    })
  })

  it.each([
    { mixin: false, script: true },
    { mixin: true, script: false },
    { mixin: false, script: false },
  ])('honors processing settings (mixin $mixin, script $script)', async ({ mixin, script }) => {
    await generateConfigViaRpcByProfile(profile, {
      enableMixinProcessing: mixin,
      enableScriptProcessing: script,
    })

    expect(mocks.rpc.generateConfig).toHaveBeenCalledExactlyOnceWith({
      profile: convertedProfile,
      options: {
        enableAlphaConfigAdaptation: false,
        enableMixinProcessing: mixin,
        enableScriptProcessing: script,
      },
    })
  })

  it.each([
    { branch: Branch.Main, adaptation: false },
    { branch: Branch.Alpha, adaptation: true },
  ])(
    'uses the same branch defaults when generating by profile ID on $branch',
    async ({ branch, adaptation }) => {
      mocks.appConfig.config.branch = branch

      await expect(generateConfigViaRpc('profile-id')).resolves.toEqual(config)

      expect(mocks.createRpcClient).toHaveBeenNthCalledWith(1, ProfileService)
      expect(mocks.createRpcClient).toHaveBeenNthCalledWith(2, KernelConfigService)
      expect(mocks.rpc.getProfile).toHaveBeenCalledExactlyOnceWith({ id: 'profile-id' })
      expect(mocks.iProfileToProto).toHaveBeenCalledExactlyOnceWith(profile)
      expect(mocks.rpc.generateConfig).toHaveBeenCalledExactlyOnceWith({
        profile: convertedProfile,
        options: {
          enableAlphaConfigAdaptation: adaptation,
          enableMixinProcessing: true,
          enableScriptProcessing: true,
        },
      })
    },
  )
})
