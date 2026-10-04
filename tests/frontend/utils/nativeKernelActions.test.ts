import { describe, expect, it, vi } from 'vitest'

import {
  changeModeThenClose,
  queueKernelAction,
  selectThenClose,
} from '@/utils/nativeKernelActions'

describe('native kernel control ordering', () => {
  it('snapshots the old group connections, selects, then closes only those IDs', async () => {
    const order: string[] = []
    await selectThenClose('proxy', 'new-node', true, {
      getSnapshot: async () => {
        order.push('snapshot')
        return [
          { id: 'old', chainList: ['old-node', 'proxy'] },
          { id: 'other', chainList: ['direct'] },
        ]
      },
      select: async (group, node) => {
        order.push(`select:${group}:${node}`)
      },
      close: async (id) => {
        order.push(`close:${id}`)
      },
    })
    expect(order).toEqual(['snapshot', 'select:proxy:new-node', 'close:old'])
  })

  it('does not close any connection when outbound selection fails', async () => {
    const close = vi.fn()
    await expect(
      selectThenClose('proxy', 'node', true, {
        getSnapshot: async () => [{ id: 'old', chainList: ['proxy'] }],
        select: async () => {
          throw new Error('selection failed')
        },
        close,
      }),
    ).rejects.toThrow('selection failed')
    expect(close).not.toHaveBeenCalled()
  })

  it('does not close connections after mismatched mode readback', async () => {
    const closeAll = vi.fn()
    await expect(
      changeModeThenClose('rule', {
        setMode: async () => {
          return { currentMode: 'global' }
        },
        closeAll,
      }),
    ).rejects.toThrow()
    expect(closeAll).not.toHaveBeenCalled()
  })

  it('serializes repeated actions and lets the next action run after rejection', async () => {
    let release!: () => void
    const wait = new Promise<void>((resolve) => {
      release = resolve
    })
    const order: string[] = []
    const first = queueKernelAction('ordering-test', async () => {
      order.push('first:start')
      await wait
      order.push('first:failed')
      throw new Error('failed')
    })
    const rejection = expect(first).rejects.toThrow('failed')
    const second = queueKernelAction('ordering-test', async () => {
      order.push('second')
    })
    await Promise.resolve()
    await Promise.resolve()
    expect(order).toEqual(['first:start'])
    release()
    await rejection
    await second
    expect(order).toEqual(['first:start', 'first:failed', 'second'])
  })
})
