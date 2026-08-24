import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, KeyRound, LoaderCircle, LogIn, Save } from 'lucide-react'
import { api, APIError } from '../api'
import type { MCPServer } from '../types'

function parsePairs(value: string) {
  const output: Record<string, string> = {}
  for (const line of value.split('\n')) {
    const trimmed = line.trim(); if (!trimmed || trimmed.startsWith('#')) continue
    const index = trimmed.indexOf('='); if (index <= 0) throw new Error(`Expected NAME=value: ${trimmed}`)
    output[trimmed.slice(0, index).trim()] = trimmed.slice(index + 1)
  }
  return output
}

export default function MCPConnectionEditor({ agentID, server }: { agentID: number; server: MCPServer }) {
  const client = useQueryClient()
  const [environment, setEnvironment] = useState('')
  const [headers, setHeaders] = useState('')
  const [parseError, setParseError] = useState<string>()
  const connection = useQuery({ queryKey: ['mcp-connection', agentID, server.id], queryFn: () => api.mcpConnection(agentID, server.id) })
  const save = useMutation({
    mutationFn: () => { setParseError(undefined); try { return api.updateMCPCredentials(agentID, server.id, parsePairs(environment), parsePairs(headers)) } catch (error) { setParseError(error instanceof Error ? error.message : 'Invalid values'); throw error } },
    onSuccess: data => { client.setQueryData(['mcp-connection', agentID, server.id], data); setEnvironment(''); setHeaders('') },
  })
  const oauth = useMutation({ mutationFn: () => api.startMCPOAuth(agentID, server.id), onSuccess: data => { window.location.assign(data.authorization_url) } })
  return <details className="tool-call mcp-connection"><summary><KeyRound size={15} /> {server.name}<span className="provider-meta">{connection.data?.credential_set && <em>Credentials set</em>}{connection.data?.oauth_connected && <em>OAuth connected</em>}</span></summary><div className="tool-detail">
    <p className="field-note">Values are encrypted per Agent and never returned. Saving replaces the current environment and headers.</p>
    <div className="form-grid"><label><span>Environment</span><textarea rows={4} value={environment} onChange={e => setEnvironment(e.target.value)} placeholder={'API_TOKEN=…\nREGION=…'} /></label>{server.transport === 'http' && <label><span>HTTP headers</span><textarea rows={4} value={headers} onChange={e => setHeaders(e.target.value)} placeholder={'Authorization=Bearer …\nX-API-Key=…'} /></label>}</div>
    {(parseError || save.error || oauth.error) && <div className="form-error">{parseError || (save.error instanceof APIError ? save.error.message : oauth.error instanceof APIError ? oauth.error.message : 'Could not update the connection.')}</div>}
    <div className="test-row"><button type="button" className="button secondary" onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? <LoaderCircle className="spin" size={15} /> : connection.data?.credential_set ? <Check size={15} /> : <Save size={15} />} Save credentials</button>{server.oauth_enabled && <button type="button" className="button secondary" onClick={() => oauth.mutate()} disabled={oauth.isPending}>{oauth.isPending ? <LoaderCircle className="spin" size={15} /> : <LogIn size={15} />} {connection.data?.oauth_connected ? 'Reconnect OAuth' : 'Connect OAuth'}</button>}</div>
  </div></details>
}
