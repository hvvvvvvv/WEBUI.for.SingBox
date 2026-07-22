import { Code, ConnectError } from '@connectrpc/connect'

import type { ExpectedRevision, MutationState, ResourceState } from '../../gen/common/v1/sync_pb'

export type ResourceDomain = 'profiles' | 'subscriptions' | 'rulesets' | 'scheduledTasks'

export interface LocalResourceState {
  instanceId: string
  stateRevision: bigint
  orderRevision: bigint
  itemRevisions: Record<string, bigint>
}

export interface ResourceChangedEvent {
  domain: ResourceDomain
  operation: 'upsert' | 'delete' | 'reorder' | 'runtime'
  ids: string[]
  instanceId: string
  stateRevision: number
}

export const createLocalResourceState = (): LocalResourceState => ({
  instanceId: '',
  stateRevision: 0n,
  orderRevision: 0n,
  itemRevisions: {},
})

export const applyResourceSnapshot = (
  target: LocalResourceState,
  source?: ResourceState,
): boolean => {
  if (!source?.instanceId) return false
  if (target.instanceId === source.instanceId && source.stateRevision < target.stateRevision) {
    return false
  }
  target.instanceId = source.instanceId
  target.stateRevision = source.stateRevision
  target.orderRevision = source.orderRevision
  target.itemRevisions = { ...source.itemRevisions }
  return true
}

export const applyMutationState = (
  target: LocalResourceState,
  source: MutationState | undefined,
  options: { id?: string; deleted?: boolean } = {},
): boolean => {
  if (!source?.instanceId) return false
  if (target.instanceId && target.instanceId !== source.instanceId) return false
  if (source.stateRevision < target.stateRevision) return false

  target.instanceId = source.instanceId
  target.stateRevision = source.stateRevision
  target.orderRevision = source.orderRevision
  if (options.id) {
    if (options.deleted) {
      delete target.itemRevisions[options.id]
    } else if (source.itemRevision > 0n) {
      target.itemRevisions[options.id] = source.itemRevision
    }
  }
  return true
}

export const expectedItemRevision = (
  state: LocalResourceState,
  id: string,
): Pick<ExpectedRevision, 'instanceId' | 'revision'> => ({
  instanceId: state.instanceId,
  revision: state.itemRevisions[id] ?? 0n,
})

export const expectedOrderRevision = (
  state: LocalResourceState,
): Pick<ExpectedRevision, 'instanceId' | 'revision'> => ({
  instanceId: state.instanceId,
  revision: state.orderRevision,
})

export const eventRevisionApplied = (state: LocalResourceState, event: ResourceChangedEvent) =>
  state.instanceId === event.instanceId && state.stateRevision >= BigInt(event.stateRevision)

export const isResourceConflict = (error: unknown) =>
  error instanceof ConnectError &&
  (error.code === Code.Aborted || error.code === Code.FailedPrecondition)

export const isResourceNotFound = (error: unknown) =>
  error instanceof ConnectError && error.code === Code.NotFound
