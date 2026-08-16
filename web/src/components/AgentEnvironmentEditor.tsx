import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Eye, EyeOff, KeyRound, LoaderCircle, Plus, Save, Trash2 } from 'lucide-react'
import { api, APIError } from '../api'
import type { AgentEnvironmentWrite } from '../types'

type DraftVariable = AgentEnvironmentWrite & { id: string }

export default function AgentEnvironmentEditor({ agentID, sandboxID }: { agentID?: number; sandboxID: number | null }) {
  const client = useQueryClient()
  const [draft, setDraft] = useState<DraftVariable[]>([])
  const [revealed, setRevealed] = useState<Record<string, boolean>>({})
  const [dirty, setDirty] = useState(false)
  const [saved, setSaved] = useState(false)
  const query = useQuery({ queryKey: ['agent-environment', agentID], queryFn: () => api.agentEnvironment(agentID!), enabled: !!agentID, retry: false })
  useEffect(() => {
    if (!query.data) return
    setDraft(query.data.variables.map((item, index) => ({ id: `${item.name}-${index}`, name: item.name, value: item.value || '', secret: item.secret, keep_current: item.secret && item.has_value })))
    setDirty(false)
  }, [query.data])
  const save = useMutation({
    mutationFn: () => api.updateAgentEnvironment(agentID!, draft.map(item => ({ name: item.name, value: item.value, secret: item.secret, keep_current: item.keep_current }))),
    onSuccess: data => { client.setQueryData(['agent-environment', agentID], data); setSaved(true); setTimeout(() => setSaved(false), 2000) },
  })
  if (!agentID) return <div className="empty-inline"><KeyRound /><div><b>Save the agent first</b><p>Environment variables are encrypted and attached to a saved agent.</p></div></div>
  const update = (id: string, values: Partial<DraftVariable>) => { setDraft(items => items.map(item => item.id === id ? { ...item, ...values } : item)); setDirty(true) }
  return <div className="environment-editor">
    {!sandboxID && <div className="inline-alert warning"><KeyRound /><div><b>No Sandbox selected</b><p>Variables can be saved now, but they are only injected when this agent runs a Sandbox process.</p></div></div>}
    <div className="environment-list">{draft.map(item => <div className="environment-row" key={item.id}>
      <label className="field"><span>Name</span><input className="code-textarea" value={item.name} onChange={event => update(item.id, { name: event.target.value.toUpperCase() })} placeholder="MYSQL_DSN" /></label>
      <label className="field environment-value"><span>Value {item.keep_current && <em>Stored secret</em>}</span><input className="code-textarea" type={item.secret && !revealed[item.id] ? 'password' : 'text'} value={item.value || ''} onChange={event => update(item.id, { value: event.target.value, keep_current: false })} placeholder={item.keep_current ? 'Leave blank to keep current value' : 'Value'} /><button type="button" aria-label={revealed[item.id] ? 'Hide value' : 'Show value'} onClick={() => setRevealed(values => ({ ...values, [item.id]: !values[item.id] }))}>{revealed[item.id] ? <EyeOff /> : <Eye />}</button></label>
      <label className="environment-secret"><input type="checkbox" checked={item.secret} onChange={event => update(item.id, { secret: event.target.checked })} /> Secret</label>
      <button type="button" className="icon-button danger-text" onClick={() => { setDraft(items => items.filter(value => value.id !== item.id)); setDirty(true) }} aria-label={`Remove ${item.name || 'variable'}`}><Trash2 /></button>
    </div>)}</div>
    {!draft.length && <div className="empty-inline"><KeyRound /><div><b>No environment variables</b><p>Add database connection settings or other per-agent configuration.</p></div></div>}
    <div className="environment-actions"><button type="button" className="button secondary" onClick={() => { setDraft(items => [...items, { id: crypto.randomUUID(), name: '', value: '', secret: true, keep_current: false }]); setDirty(true) }}><Plus /> Add variable</button><button type="button" className="button primary" disabled={!dirty || save.isPending || draft.some(item => !item.name.trim())} onClick={() => save.mutate()}>{save.isPending ? <LoaderCircle className="spin" /> : saved ? <Check /> : <Save />}{saved ? 'Saved' : 'Save environment'}</button></div>
    {save.error && <div className="form-error">{save.error instanceof APIError ? save.error.message : 'Could not save environment variables.'}</div>}
    <p className="field-note">Secret values are encrypted at rest, never returned by the API, and masked from recorded logs where exact values appear. Agents sharing one Sandbox are mutually trusted and can inspect one another's processes.</p>
  </div>
}
