import { useQuery } from '@tanstack/react-query'
import { ArrowRight, Boxes, CircleAlert, Cloud, Plus, RefreshCw } from 'lucide-react'
import { Link } from 'react-router-dom'
import { api } from '../api'

export default function Providers({ embedded = false }: { embedded?: boolean }) {
  const providers = useQuery({ queryKey: ['providers', true], queryFn: () => api.providers(true) })
  const Heading = embedded ? 'h2' : 'h1'
  const content = <>
    <header className="page-header"><div><div className="eyebrow">Connections</div><Heading>Providers</Heading><p className="muted">Configure an endpoint once, then share it safely across agents.</p></div><Link to="/settings/providers/new" className="button primary"><Plus size={16} /> New provider</Link></header>
    {providers.isLoading ? <div className="provider-grid"><div className="skeleton tall" /><div className="skeleton tall" /></div> : providers.data?.providers.length ? <div className="provider-grid">{providers.data.providers.map(provider => <Link key={provider.id} to={`/settings/providers/${provider.id}/edit`} className={`provider-card ${provider.archived_at ? 'archived' : ''}`}>
      <div className="provider-card-icon"><Cloud /></div><div className="provider-card-main"><div className="provider-card-title"><h2>{provider.name}</h2>{provider.archived_at && <span className="status-pill">Archived</span>}</div><code>{provider.base_url}</code><div className="provider-card-meta"><span><Boxes size={14} /> {provider.model_count} models</span><span>{provider.prompt_format === 'claude' ? 'Claude / XML' : 'OpenAI / Markdown'}</span>{provider.models_last_error ? <span className="warning-text"><CircleAlert size={14} /> Refresh failed</span> : provider.catalog_stale ? <span><RefreshCw size={14} /> Refresh recommended</span> : <span className="success-text">Catalog current</span>}</div></div><ArrowRight className="provider-arrow" />
    </Link>)}</div> : <section className="empty-state card"><Cloud /><h2>No providers yet</h2><p>Add your first OpenAI-compatible endpoint before creating an agent.</p><Link to="/settings/providers/new" className="button primary"><Plus size={16} /> Add provider</Link></section>}
  </>
  return embedded ? content : <main className="page scroll-page settings-page">{content}</main>
}
