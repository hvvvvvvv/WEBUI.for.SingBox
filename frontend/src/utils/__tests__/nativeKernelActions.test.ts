import { describe, expect, it, vi } from 'vitest'

import { changeModeThenClose, queueKernelAction, selectThenClose } from '../nativeKernelActions'

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

  it('does not select when the fresh connection snapshot fails', async () => {
    const select = vi.fn()
    const close = vi.fn()
    await expect(
      selectThenClose('proxy', 'node', true, {
        getSnapshot: async () => {
          throw new Error('snapshot failed')
        },
        select,
        close,
      }),
    ).rejects.toThrow('snapshot failed')
    expect(select).not.toHaveBeenCalled()
    expect(close).not.toHaveBeenCalled()
  })

  it('does not open a snapshot stream when auto-close is disabled', async () => {
    const getSnapshot = vi.fn()
    const close = vi.fn()
    const select = vi.fn().mockResolvedValue(undefined)
    await selectThenClose('proxy', 'node', false, { getSnapshot, select, close })
    expect(select).toHaveBeenCalledWith('proxy', 'node')
    expect(getSnapshot).not.toHaveBeenCalled()
    expect(close).not.toHaveBeenCalled()
  })

  it('waits for a matching mode readback before closing all connections', async () => {
    const order: string[] = []
    await changeModeThenClose('Rule', {
      setMode: async (mode) => {
        order.push(mode)
        return { currentMode: 'rule' }
      },
      closeAll: async () => {
        order.push('closeAll')
      },
    })
    expect(order).toEqual(['Rule', 'closeAll'])
  })

  it.each(['failed', 'mismatch'])(
    'does not close connections after %s mode update',
    async (failure) => {
      const closeAll = vi.fn()
      await expect(
        changeModeThenClose('rule', {
          setMode: async () => {
            if (failure === 'failed') throw new Error('failed')
            return { currentMode: 'global' }
          },
          closeAll,
        }),
      ).rejects.toThrow()
      expect(closeAll).not.toHaveBeenCalled()
    },
  )

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
