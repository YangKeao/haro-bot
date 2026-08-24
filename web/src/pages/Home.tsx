import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Archive, ArrowRight, BarChart3, BookOpenText, Bot, BrainCircuit, Clock3, Code2, LoaderCircle,
  MessageCircle, Plus, RotateCcw, Search, Sparkles, Terminal,
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import type { AgentProfile } from '../types'
import InlineMarkdown from '../components/InlineMarkdown'

const avatarIcons = {
  sparkles: Sparkles, bot: Bot, search: Search, research: Search, code: Code2,
  book: BookOpenText, chart: BarChart3, terminal: Terminal, brain: BrainCircuit,
}

type AvatarAgent = Pick<AgentProfile, 'name' | 'color' | 'icon' | 'avatar_mode' | 'avatar_url'>

export function AgentAvatar({ agent, size = 'normal' }: { agent: AvatarAgent; size?: 'normal' | 'large' }) {
  const Icon = avatarIcons[agent.icon as keyof typeof avatarIcons] || Sparkles
  return <div className={`agent-avatar ${size}`} style={{ '--agent-color': agent.color } as React.CSSProperties} aria-label={`${agent.name} avatar`}>
    {agent.avatar_mode === 'image' && agent.avatar_url ? <img src={agent.avatar_url} alt="" /> : <Icon />}
  </div>
}

export default function Home() {
  const client = useQueryClient()
  const [showArchived, setShowArchived] = useState(false)
  const agents = useQuery({ queryKey: ['agents'], queryFn: () => api.agents() })
  const recent = useQuery({ queryKey: ['recent-sessions', 6], queryFn: () => api.recentSessions(6) })
  const archived = useQuery({ queryKey: ['agents', 'archived'], queryFn: () => api.agents(true), enabled: showArchived })
  const archivedAgents = archived.data?.agents.filter(agent => agent.archived_at) || []
  const restore = useMutation({
    mutationFn: (id: number) => api.archiveAgent(id, true),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: ['agents'] })
      client.invalidateQueries({ queryKey: ['recent-sessions'] })
    },
  })
  return <main className="page scroll-page">
    <header className="page-header home-header">
      <div><div className="eyebrow">Your workspace</div><h1>Welcome back.</h1><p className="muted">Pick up a conversation or start something new.</p></div>
      <div className="home-actions"><button className="button secondary" onClick={() => setShowArchived(value => !value)}><Archive size={16} /> Archived</button><Link to="/agents/new" className="button primary"><Plus size={17} /> New agent</Link></div>
    </header>

    <section className="recent-section" aria-labelledby="recent-heading">
      <div className="section-toolbar simple"><div><div className="section-kicker">Continue working</div><h2 id="recent-heading">Recent conversations</h2></div>{recent.data?.sessions.length ? <span className="section-count">{recent.data.sessions.length} recent</span> : null}</div>
      {recent.isLoading ? <div className="recent-list">{[1, 2, 3].map(item => <div className="recent-row skeleton" key={item} />)}</div> : recent.data?.sessions.length ? <div className="recent-list">{recent.data.sessions.map(session => <Link className="recent-row" key={session.id} to={`/agents/${session.agent_id}/sessions/${session.id}`}>
        <AgentAvatar agent={session.agent} />
        <div className="recent-main"><b><InlineMarkdown>{session.title}</InlineMarkdown></b><span>{session.agent.name}</span></div>
        <time dateTime={session.updated_at}><Clock3 /> {formatRelative(session.updated_at)}</time>
        <ArrowRight className="recent-arrow" />
      </Link>)}</div> : <div className="recent-empty"><MessageCircle /><div><b>No conversations yet</b><p>Choose an agent below to begin your first session.</p></div></div>}
    </section>

    <section className="agents-section" aria-labelledby="agents-heading">
      <div className="section-toolbar simple"><div><div className="section-kicker">Your team</div><h2 id="agents-heading">Agents</h2></div></div>
    {agents.isLoading ? <div className="card-grid">{[1, 2, 3].map(i => <div key={i} className="agent-card skeleton tall" />)}</div> : agents.data?.agents.length ?
      <div className="card-grid" aria-label="Agents">
        {agents.data.agents.map(agent => <Link key={agent.id} to={`/agents/${agent.id}/sessions`} className="agent-card" aria-label={`Open ${agent.name}`}>
          <div className="agent-card-top"><AgentAvatar agent={agent} size="large" /><ArrowRight className="card-arrow" size={19} /></div>
          <h2>{agent.name}</h2><p>{agent.description || 'Ready for a new conversation.'}</p>
          <div className="agent-meta"><span>{agent.model}</span><span>{agent.skill_names.length} skills</span></div>
        </Link>)}
        <Link to="/agents/new" className="agent-card new-card"><div className="new-agent-icon"><Plus /></div><h2>Create an agent</h2><p>Give a specialist its own model, instructions, and skills.</p></Link>
      </div> :
      <section className="empty-hero"><div className="empty-orbit"><Bot size={31} /></div><h2>Build your first agent</h2><p>Configure a provider, shape its behavior, and choose exactly which skills it can use.</p><Link to="/agents/new" className="button primary"><Plus size={17} /> Create agent</Link></section>}
    </section>
    {showArchived && <section className="archived-agents" aria-label="Archived agents">
      <div className="section-heading"><h2>Archived agents</h2><p>Restore an agent to make its conversations available again.</p></div>
      {archived.isLoading ? <div className="archived-agent-row skeleton" /> : archivedAgents.length ? <div className="archived-agent-list">{archivedAgents.map(agent => <article className="archived-agent-row" key={agent.id}><AgentAvatar agent={agent} /><div><b>{agent.name}</b><span>{agent.model}</span></div><button className="button secondary small" disabled={restore.isPending} onClick={() => restore.mutate(agent.id)}>{restore.isPending ? <LoaderCircle className="spin" size={14} /> : <RotateCcw size={14} />} Restore</button></article>)}</div> : <div className="empty-inline"><Archive /><div><b>No archived agents</b><p>Agents you archive will appear here.</p></div></div>}
    </section>}
  </main>
}

function formatRelative(value: string) {
  const diff = Date.now() - new Date(value).getTime()
  if (diff < 60_000) return 'Just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(new Date(value))
}
