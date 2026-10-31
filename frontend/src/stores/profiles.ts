import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'

import { createRpcClient } from '@/bridge'
import * as Defaults from '@/constant/profile'
import { useAppConfigStore } from '@/stores'
import { sampleID } from '@/utils/others'
import { iProfileToProto, protoProfileToIProfile } from '@/utils/profileRpc'
import { ProfileService } from '../../gen/profile/v1/profile_service_pb'
import {
  applyMutationState,
  applyResourceSnapshot,
  createLocalResourceState,
  expectedItemRevision,
  expectedOrderRevision,
} from './resourceSync'

import type { ExpectedRevision } from '../../gen/common/v1/sync_pb'

type Revision = Pick<ExpectedRevision, 'instanceId' | 'revision'>

export const useProfilesStore = defineStore('profiles', () => {
  const appConfigStore = useAppConfigStore()
  const service = createRpcClient(ProfileService)

  const profiles = ref<IProfile[]>([])
  const resourceState = reactive(createLocalResourceState())
  const currentProfile = computed(() => getProfileById(appConfigStore.config.profile))
  let setupRequestID = 0
  let latestAppliedSetupRequestID = 0

  const setupProfiles = async () => {
    const requestID = ++setupRequestID
    const { profiles: items, state } = await service.listProfiles({})
    const next = items.map((item) => protoProfileToIProfile(item))
    if (
      state?.instanceId &&
      resourceState.instanceId &&
      state.instanceId !== resourceState.instanceId &&
      requestID < latestAppliedSetupRequestID
    ) {
      return
    }
    if (applyResourceSnapshot(resourceState, state)) {
      latestAppliedSetupRequestID = Math.max(latestAppliedSetupRequestID, requestID)
      profiles.value = next
    }
  }

  const applyProfileMutation = async (
    state: Parameters<typeof applyMutationState>[1],
    options: { id?: string; deleted?: boolean } = {},
  ) => {
    if (applyMutationState(resourceState, state, options)) return true
    if (state?.instanceId && state.instanceId !== resourceState.instanceId) {
      await setupProfiles()
    }
    return false
  }

  const saveProfilesOrder = async (
    ids: string[],
    revision: Revision = expectedOrderRevision(resourceState),
    fallbackIDs: string[] = [],
  ) => {
    try {
      const { ids: orderedIDs, state } = await service.reorderProfiles({
        ids,
        expectedOrderRevision: revision,
      })
      if (!(await applyProfileMutation(state))) return
      const byId = new Map(profiles.value.map((item) => [item.id, item]))
      const ordered = orderedIDs.flatMap((id) => byId.get(id) || [])
      if (ordered.length !== profiles.value.length) {
        await setupProfiles()
        return
      }
      profiles.value.splice(0, profiles.value.length, ...ordered)
    } catch (error) {
      try {
        await setupProfiles()
      } catch {
        const byId = new Map(profiles.value.map((item) => [item.id, item]))
        const fallback = fallbackIDs.flatMap((id) => byId.get(id) || [])
        if (fallback.length === profiles.value.length) {
          profiles.value.splice(0, profiles.value.length, ...fallback)
        }
      }
      throw error
    }
  }

  const addProfile = async (p: IProfile) => {
    const { profile, state } = await service.createProfile({
      profile: iProfileToProto(p),
    })
    if (!profile || !(await applyProfileMutation(state, { id: p.id }))) return
    const item = protoProfileToIProfile(profile)
    const idx = profiles.value.findIndex((value) => value.id === item.id)
    if (idx === -1) profiles.value.push(item)
    else profiles.value.splice(idx, 1, item)
  }

  const deleteProfile = async (
    id: string,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { state } = await service.deleteProfile({ id, expectedRevision: revision })
    if (!(await applyProfileMutation(state, { id, deleted: true }))) return
    const idx = profiles.value.findIndex((item) => item.id === id)
    if (idx !== -1) profiles.value.splice(idx, 1)
  }

  const editProfile = async (
    id: string,
    p: IProfile,
    revision: Revision = expectedItemRevision(resourceState, id),
  ) => {
    const { profile, state } = await service.updateProfile({
      profile: iProfileToProto({ ...p, id }),
      expectedRevision: revision,
    })
    if (!profile || !(await applyProfileMutation(state, { id }))) return
    const item = protoProfileToIProfile(profile)
    const idx = profiles.value.findIndex((value) => value.id === id)
    if (idx === -1) profiles.value.push(item)
    else profiles.value.splice(idx, 1, item)
  }

  const getProfileById = (id: string) => profiles.value.find((v) => v.id === id)
  const getProfileRevision = (id: string) => expectedItemRevision(resourceState, id)
  const getProfilesOrderRevision = () => expectedOrderRevision(resourceState)

  const getProfileTemplate = (name = ''): IProfile => ({
    id: sampleID(),
    name,
    log: Defaults.DefaultLog(),
    experimental: Defaults.DefaultExperimental(),
    inbounds: Defaults.DefaultInbounds(),
    outbounds: Defaults.DefaultOutbounds(),
    route: Defaults.DefaultRoute(),
    dns: Defaults.DefaultDns(),
    mixin: Defaults.DefaultMixin(),
    script: Defaults.DefaultScript(),
  })

  return {
    profiles,
    resourceState,
    currentProfile,
    setupProfiles,
    saveProfilesOrder,
    addProfile,
    editProfile,
    deleteProfile,
    getProfileById,
    getProfileRevision,
    getProfilesOrderRevision,
    getProfileTemplate,
  }
})
