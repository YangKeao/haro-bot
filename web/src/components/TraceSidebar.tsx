import { Check, ChevronDown, CircleAlert, Globe2, LoaderCircle, MessageSquareText, Sparkles, Wrench, X } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { TraceStep } from '../types'

export default function TraceSidebar({ steps, running, status, onClose }: { steps: TraceStep[]; running: boolean; status?: string; onClose: () => void }) {
  return <>
    <button className="trace-sidebar-overlay" onClick={onClose} aria-label="Close thinking process" />
    <aside className="trace-sidebar" aria-label="Thinking process">
      <header className="trace-sidebar-header">
        <div><span className="eyebrow">Activity</span><h2>Thinking process</h2><p>{running ? 'Updating live' : status === 'error' || status === 'cancelled' ? 'Run interrupted' : `${steps.length} step${steps.length === 1 ? '' : 's'}`}</p></div>
        <button className="icon-button" onClick={onClose} aria-label="Close thinking process"><X /></button>
      </header>
      <div className="trace-sidebar-body">
        {!steps.length ? <div className="trace-empty"><LoaderCircle className={running ? 'spin' : ''} /><p>{running ? 'Waiting for the first activity…' : 'No thinking details were recorded.'}</p></div> : <div className="trace-timeline">
          {steps.map(step => <TraceEntry key={step.id} step={step} />)}
        </div>}
      </div>
    </aside>
  </>
}

function TraceEntry({ step }: { step: TraceStep }) {
  if (step.kind === 'reasoning') return <article className="trace-entry reasoning-step">
    <TraceMarker step={step} fallback={<Sparkles />} />
    <div className="trace-entry-card"><div className="trace-entry-heading"><span>Reasoning summary</span><StatusLabel step={step} /></div>
      {step.content ? <div className="trace-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{step.content}</ReactMarkdown></div> : <p className="trace-waiting">Reasoning…</p>}
    </div>
  </article>

  if (step.kind === 'commentary') return <article className="trace-entry commentary-step">
    <TraceMarker step={step} fallback={<MessageSquareText />} />
    <div className="trace-entry-card"><div className="trace-entry-heading"><span>Model note</span><StatusLabel step={step} /></div>
      <div className="trace-markdown"><ReactMarkdown remarkPlugins={[remarkGfm]}>{step.content || ''}</ReactMarkdown></div>
    </div>
  </article>

  const active = step.status === 'preparing' || step.status === 'running' || step.status === 'searching'
  const hosted = step.tool_kind === 'hosted'
  return <article className="trace-entry tool-step">
    <TraceMarker step={step} fallback={hosted ? <Globe2 /> : <Wrench />} />
    <details className="trace-entry-card trace-tool" open={active}>
      <summary><span>{toolLabel(step)}</span><StatusLabel step={step} /><ChevronDown /></summary>
      <div className="trace-tool-detail">
        {step.arguments && <DetailBlock label="Arguments"><pre>{prettyJSON(step.arguments)}</pre></DetailBlock>}
        {hosted && <HostedToolDetail detail={step.detail} />}
        {step.result && <DetailBlock label={`Result${step.truncated ? ' (preview)' : ''}`}><pre>{step.result}</pre></DetailBlock>}
        {!hosted && step.detail !== undefined && step.detail !== null && <DetailBlock label="Structured result"><pre>{JSON.stringify(step.detail, null, 2)}</pre></DetailBlock>}
        {!step.arguments && !step.result && !step.detail && active && <p className="trace-waiting">Waiting for tool activity…</p>}
      </div>
    </details>
  </article>
}

function TraceMarker({ step, fallback }: { step: TraceStep; fallback: React.ReactNode }) {
  const active = step.status === 'preparing' || step.status === 'running' || step.status === 'searching'
  return <span className={`trace-marker ${step.status || ''}`}>{active ? <LoaderCircle className="spin" /> : step.status === 'error' || step.status === 'cancelled' ? <CircleAlert /> : fallback}</span>
}

function StatusLabel({ step }: { step: TraceStep }) {
  const active = step.status === 'preparing' || step.status === 'running' || step.status === 'searching'
  return <small className={`trace-status ${step.status || ''}`}>{active ? <LoaderCircle className="spin" /> : step.status === 'error' || step.status === 'cancelled' ? <CircleAlert /> : <Check />}{statusText(step.status)}</small>
}

function HostedToolDetail({ detail }: { detail: unknown }) {
  const root = record(detail)
  const action = record(root?.action)
  const query = typeof action?.query === 'string' ? action.query : undefined
  const url = typeof action?.url === 'string' ? action.url : undefined
  const pattern = typeof action?.pattern === 'string' ? action.pattern : undefined
  const links = collectLinks(detail)
  if (!query && !url && !pattern && !links.length && detail === undefined) return null
  return <>
    {(query || url || pattern) && <DetailBlock label={typeof action?.type === 'string' ? action.type.replace('_', ' ') : 'Web action'}>
      {query && <p className="trace-web-value">{query}</p>}
      {url && <a className="trace-source" href={url} target="_blank" rel="noreferrer">{url}</a>}
      {pattern && <code>{pattern}</code>}
    </DetailBlock>}
    {links.length > 0 && <DetailBlock label="Sources"><div className="trace-sources">{links.map(link => <a key={link.url} href={link.url} target="_blank" rel="noreferrer"><Globe2 /> <span>{link.title || link.url}</span></a>)}</div></DetailBlock>}
  </>
}

function DetailBlock({ label, children }: { label: string; children: React.ReactNode }) {
  return <section className="trace-detail-block"><b>{label}</b>{children}</section>
}

function toolLabel(step: TraceStep) {
  if (step.name === 'web_search') return 'Web search'
  return step.name || 'Tool call'
}

function statusText(status?: TraceStep['status']) {
  if (status === 'preparing') return 'Preparing'
  if (status === 'running') return 'Running'
  if (status === 'searching') return 'Searching'
  if (status === 'error') return 'Failed'
  if (status === 'cancelled') return 'Cancelled'
  return 'Completed'
}

function prettyJSON(value: string) {
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}

function record(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined
}

function collectLinks(value: unknown, depth = 0, found = new Map<string, string>()) {
  if (depth > 5 || value === null || value === undefined) return [...found].map(([url, title]) => ({ url, title }))
  if (Array.isArray(value)) for (const item of value) collectLinks(item, depth + 1, found)
  else {
    const item = record(value)
    if (item) {
      const url = typeof item.url === 'string' && /^https?:\/\//.test(item.url) ? item.url : undefined
      if (url) found.set(url, typeof item.title === 'string' ? item.title : '')
      for (const child of Object.values(item)) collectLinks(child, depth + 1, found)
    }
  }
  return [...found].map(([url, title]) => ({ url, title }))
}
