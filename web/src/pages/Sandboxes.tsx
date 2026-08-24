import { useQuery } from '@tanstack/react-query'
import { ArrowRight, Box, CircleAlert, Cpu, Database, HardDrive, MemoryStick, Plus, Radio } from 'lucide-react'
import { Link } from 'react-router-dom'
import { api, APIError } from '../api'
import { useSandboxEvents } from '../sandboxEvents'

export default function Sandboxes() {
  const query = useQuery({ queryKey: ['sandboxes'], queryFn: api.sandboxes, retry: false })
  const stream = useSandboxEvents(Boolean(query.data))
  const disabled = query.error instanceof APIError && query.error.code === 'sandbox_disabled'
  return <main className="page scroll-page settings-page">
    <header className="page-header"><div><div className="eyebrow">Execution</div><h1>Sandboxes</h1><p className="muted">Persistent, isolated Kubernetes workspaces that one or more agents can share.</p></div><div className="sandbox-header-actions"><span className={`live-indicator ${stream}`}><Radio /> {stream === 'live' ? 'Live' : stream === 'connecting' ? 'Connecting' : 'Reconnecting'}</span><Link to="/sandboxes/new" className={`button primary ${disabled ? 'disabled-link' : ''}`} aria-disabled={disabled}><Plus size={16} /> New sandbox</Link></div></header>
    {disabled ? <div className="empty-state card"><CircleAlert /><h2>Sandbox support is disabled</h2><p>Enable the <code>[sandbox]</code> configuration and provide <code>HARO_SANDBOX_SECRET_KEY</code>, then restart Haro.</p></div> : query.isError ? <div className="form-error">{query.error instanceof APIError ? query.error.message : 'Could not load sandboxes.'}</div> :
      <div className="sandbox-grid">{query.isLoading ? [1, 2].map(i => <div className="sandbox-card skeleton" key={i} />) : query.data?.sandboxes.length ? query.data.sandboxes.map(item => <Link className="sandbox-card" key={item.id} to={`/sandboxes/${item.id}/edit`}>
        <div className="sandbox-card-head"><span className="sandbox-card-icon"><Box /></span><span className={`status-pill ${statusClass(item.runtime_status)}`}>{item.runtime_status || item.desired_state}</span></div>
        <h2>{item.name}</h2><p>{item.description || item.image}</p>
        <div className="sandbox-facts"><span><Cpu /> {formatCPU(item.cpu_limit_millis)}</span><span><MemoryStick /> {formatMiB(item.memory_limit_mib)}</span><span><HardDrive /> {formatMiB(item.workspace_storage_mib)}</span><span><Database /> {item.agent_ids.length} agent{item.agent_ids.length === 1 ? '' : 's'}</span></div>
        {item.runtime_details?.message && item.runtime_status !== 'Ready' && <div className="sandbox-runtime-message">{item.runtime_details.message}</div>}{item.pending_restart && <div className="pending-change">Changes waiting to be applied</div>}{item.last_error && <div className="sandbox-error">{item.last_error}</div>}<ArrowRight className="sandbox-arrow" />
      </Link>) : <Link to="/sandboxes/new" className="empty-state card sandbox-empty"><Box /><h2>Create an execution workspace</h2><p>Choose an OCI image, resource limits, persistent storage, and which agents may use it.</p><span className="button primary"><Plus size={16} /> New sandbox</span></Link>}</div>}
  </main>
}

function formatCPU(millis: number) { return millis >= 1000 ? `${millis / 1000} CPU` : `${millis}m CPU` }
function formatMiB(value: number) { return value >= 1024 ? `${Number((value / 1024).toFixed(1))} GiB` : `${value} MiB` }
function statusClass(status: string) { return status === 'Ready' ? 'success' : status === 'Error' || status === 'Unavailable' ? 'danger' : 'warning' }
