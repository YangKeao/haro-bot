import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bot, Check, CircleAlert, LoaderCircle, Save, Send } from 'lucide-react'
import { api, APIError } from '../api'

export default function Integrations() {
  const client = useQueryClient()
  const integration = useQuery({ queryKey: ['telegram-integration'], queryFn: api.telegramIntegration })
  const agents = useQuery({ queryKey: ['agents'], queryFn: () => api.agents(false) })
  const [agentID, setAgentID] = useState<number | null>(null)
  const [dirty, setDirty] = useState(false)
  const [saved, setSaved] = useState(false)
  useEffect(() => { if (integration.data) setAgentID(integration.data.agent_id) }, [integration.data])
  const save = useMutation({ mutationFn: () => api.updateTelegramIntegration(agentID), onSuccess: data => { client.setQueryData(['telegram-integration'], data); setDirty(false); setSaved(true); setTimeout(() => setSaved(false), 2200) } })
  return <main className="page scroll-page settings-page integrations-page">
    <header className="page-header"><div><div className="eyebrow">Settings</div><h1>Integrations</h1><p className="muted">Route external channels to one of your ordinary agents.</p></div></header>
    <section className="integration-card form-section"><div className="integration-icon"><Send /></div><div className="integration-main"><div className="integration-title"><div><h2>Telegram</h2><p>Messages, instructions, skills and model settings all come from the selected agent.</p></div><span className={`status-pill ${integration.data?.token_configured ? 'success' : 'warning'}`}>{integration.data?.token_configured ? 'Token configured' : 'Token missing'}</span></div>
      {!integration.data?.token_configured && <div className="inline-alert warning"><CircleAlert /><div><b>Telegram is inactive</b><p>Add <code>[telegram].token</code> to config.toml and restart Haro. You may select the agent now.</p></div></div>}
      <label className="field"><span>Telegram agent</span><select value={agentID ?? ''} onChange={event => { setAgentID(event.target.value ? Number(event.target.value) : null); setDirty(true) }}><option value="">Not configured</option>{agents.data?.agents.map(agent => <option key={agent.id} value={agent.id}>{agent.name} · {agent.provider_name}</option>)}</select><small>Switching agents starts or resumes a separate Telegram conversation for that agent.</small></label>
      {!agents.isLoading && !agents.data?.agents.length && <div className="empty-inline"><Bot /><div><b>No active agents</b><p>Create an agent before connecting Telegram.</p></div></div>}
      {save.error && <div className="form-error">{save.error instanceof APIError ? save.error.message : 'Could not save Telegram settings'}</div>}
      <div className="integration-actions"><button className="button primary" onClick={() => save.mutate()} disabled={!dirty || save.isPending}>{save.isPending ? <LoaderCircle className="spin" size={16} /> : saved ? <Check size={16} /> : <Save size={16} />}{saved ? 'Saved' : 'Save integration'}</button></div>
    </div></section>
  </main>
}
