import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

import { createRpcClient } from '@/bridge'
import * as Defaults from '@/constant/profile'
import { useAppConfigStore } from '@/stores'
import { sampleID } from '@/utils/others'
import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'
import { ProfileManagementService } from '../../gen/profile/v1/profile_management_service_pb'

export const useProfilesStore = defineStore('profiles', () => {
  const appConfigStore = useAppConfigStore()
  const service = createRpcClient(ProfileManagementService)

  const profiles = ref<IProfile[]>([])
  const currentProfile = computed(() => getProfileById(appConfigStore.config.profile))

  const setupProfiles = async () => {
    const { profiles: items } = await service.listProfiles({})
    profiles.value = items.map((item) => protoProfileToIProfile(item))
  }

  const saveProfiles = async () => {
    try {
      await service.saveProfiles({
        profiles: profiles.value.map((item) => iProfileToProto(item)),
      })
    } catch (error) {
      await setupProfiles()
      throw error
    }
  }

  const saveProfilesOrder = async (ids: string[]) => {
    const byId = new Map(profiles.value.map((item) => [item.id, item]))
    const ordered = ids.flatMap((id) => byId.get(id) || [])
    const rest = profiles.value.filter((item) => !ids.includes(item.id))

    try {
      await service.saveProfiles({
        profiles: [...ordered, ...rest].map((item) => iProfileToProto(item)),
      })
    } catch (error) {
      await setupProfiles()
      throw error
    }
  }

  const addProfile = async (p: IProfile) => {
    await service.createProfile({
      profile: iProfileToProto(p),
    })
  }

  const deleteProfile = async (id: string) => {
    await service.deleteProfile({ id })
  }

  const editProfile = async (id: string, p: IProfile) => {
    await service.updateProfile({
      profile: iProfileToProto({ ...p, id }),
    })
  }

  const getProfileById = (id: string) => profiles.value.find((v) => v.id === id)

  const getProfileTemplate = (name = ''): IProfile => {
    return {
      id: sampleID(),
      name: name,
      log: Defaults.DefaultLog(),
      experimental: Defaults.DefaultExperimental(),
      inbounds: Defaults.DefaultInbounds(),
      outbounds: Defaults.DefaultOutbounds(),
      route: Defaults.DefaultRoute(),
      dns: Defaults.DefaultDns(),
      mixin: Defaults.DefaultMixin(),
      script: Defaults.DefaultScript(),
    }
  }

  return {
    profiles,
    currentProfile,
    setupProfiles,
    saveProfiles,
    saveProfilesOrder,
    addProfile,
    editProfile,
    deleteProfile,
    getProfileById,
    getProfileTemplate,
  }
})
