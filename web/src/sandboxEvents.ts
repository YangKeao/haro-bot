import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import type { SandboxProfile, SandboxPublicConfig } from './types'

type SandboxListCache = { sandboxes: SandboxProfile[]; config: SandboxPublicConfig }
type SandboxCache = { sandbox: SandboxProfile; config: SandboxPublicConfig }

export type SandboxStreamState = 'connecting' | 'live' | 'disconnected'

export function useSandboxEvents(enabled = true): SandboxStreamState {
  const client = useQueryClient()
  const [state, setState] = useState<SandboxStreamState>('connecting')

  useEffect(() => {
    if (!enabled) return
    const source = new EventSource('/api/v1/sandboxes/events', { withCredentials: true })
    source.onopen = () => setState('live')
    source.onerror = () => setState('disconnected')
    source.addEventListener('snapshot', event => {
      setState('live')
      const payload = JSON.parse((event as MessageEvent).data) as { sandboxes: SandboxProfile[] }
      client.setQueryData<SandboxListCache>(['sandboxes'], current => current ? { ...current, sandboxes: payload.sandboxes } : current)
      for (const sandbox of payload.sandboxes) updateSandboxCache(client, sandbox)
    })
    source.addEventListener('sandbox', event => {
      setState('live')
      const { sandbox } = JSON.parse((event as MessageEvent).data) as { sandbox: SandboxProfile }
      client.setQueryData<SandboxListCache>(['sandboxes'], current => current ? { ...current, sandboxes: upsert(current.sandboxes, sandbox) } : current)
      updateSandboxCache(client, sandbox)
    })
    source.addEventListener('removed', event => {
      setState('live')
      const { id } = JSON.parse((event as MessageEvent).data) as { id: number }
      client.setQueryData<SandboxListCache>(['sandboxes'], current => current ? { ...current, sandboxes: current.sandboxes.filter(item => item.id !== id) } : current)
      client.removeQueries({ queryKey: ['sandbox', id] })
    })
    return () => source.close()
  }, [client, enabled])

  return state
}

function updateSandboxCache(client: ReturnType<typeof useQueryClient>, sandbox: SandboxProfile) {
  client.setQueryData<SandboxCache>(['sandbox', sandbox.id], current => current ? { ...current, sandbox } : current)
}

function upsert(items: SandboxProfile[], sandbox: SandboxProfile) {
  const index = items.findIndex(item => item.id === sandbox.id)
  if (index < 0) return [sandbox, ...items]
  const result = [...items]
  result[index] = sandbox
  return result
}
