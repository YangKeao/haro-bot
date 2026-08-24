import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, BookOpenText, Check, Clock3, LoaderCircle, Save } from 'lucide-react'
import { api, APIError } from '../api'
import MarkdownEditor from '../components/MarkdownEditor'

export default function Guidelines({ embedded = false }: { embedded?: boolean }) {
  const client = useQueryClient()
  const current = useQuery({ queryKey: ['guidelines'], queryFn: api.guideline })
  const history = useQuery({ queryKey: ['guidelines-history'], queryFn: api.guidelineHistory })
  const [content, setContent] = useState('')
  const [saved, setSaved] = useState(false)
  useEffect(() => { if (current.data?.guidelines) setContent(current.data.guidelines.content) }, [current.data])
  const update = useMutation({
    mutationFn: () => api.updateGuideline(content),
    onSuccess: () => { setSaved(true); setTimeout(() => setSaved(false), 2400); client.invalidateQueries({ queryKey: ['guidelines'] }); client.invalidateQueries({ queryKey: ['guidelines-history'] }) },
  })
  const Heading = embedded ? 'h2' : 'h1'
  const contentView = <>
    <header className="page-header"><div><div className="eyebrow">Workspace behavior</div><Heading>Global guideline</Heading><p className="muted">Set the baseline behavior inherited by every agent.</p></div><button className="button primary" onClick={() => update.mutate()} disabled={!content.trim() || update.isPending}>{update.isPending ? <LoaderCircle className="spin" size={16} /> : saved ? <Check size={16} /> : <Save size={16} />}{saved ? 'Saved' : 'Save version'}</button></header>
    <div className="settings-grid">
      <section className="settings-card editor-card"><div className="card-heading"><div className="heading-icon"><BookOpenText /></div><div><h2>Active guideline</h2><p>Version {current.data?.guidelines?.version || '—'}</p></div></div>
        <div className="warning-banner"><AlertTriangle size={18} /><div><b>Shared baseline</b><span>Changes here affect both Web agents and the Telegram bot. Agent-specific instructions are appended afterwards.</span></div></div>
		{current.isLoading ? <div className="skeleton editor-skeleton" /> : <MarkdownEditor value={content} onChange={setContent} ariaLabel="Global guideline" placeholder="Write the principles and constraints every Haro agent should follow…" />}
        <div className="editor-count">{content.length.toLocaleString()} characters</div>
        {update.error && <div className="form-error">{update.error instanceof APIError ? update.error.message : 'Could not update guideline'}</div>}
      </section>
      <aside className="settings-card history-card"><div className="card-heading"><div className="heading-icon subtle"><Clock3 /></div><div><h2>Version history</h2><p>Newest first</p></div></div>
        <div className="history-list">{history.isLoading ? [1, 2, 3].map(i => <div className="history-row skeleton" key={i} />) : history.data?.history.length ? history.data.history.map(item => <button key={item.id} className={item.is_active ? 'active' : ''} onClick={() => setContent(item.content)}><span>v{item.version}</span><div><b>{item.is_active ? 'Active guideline' : 'Previous version'}</b><small>{new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(item.created_at))}</small></div>{item.is_active && <Check size={15} />}</button>) : <div className="empty-inline"><Clock3 /><div><b>No history yet</b><p>Your first saved guideline appears here.</p></div></div>}</div>
      </aside>
    </div>
  </>
  return embedded ? contentView : <main className="page scroll-page settings-page">{contentView}</main>
}
