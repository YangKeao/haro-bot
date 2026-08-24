import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, Bot, ChevronDown, MessageSquarePlus, RotateCcw, Settings2 } from 'lucide-react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../api'
import InlineMarkdown from './InlineMarkdown'

type ConversationSidebarProps = {
  agentID: number
  sessionID?: number
  onNavigate?: () => void
}

export default function ConversationSidebar({ agentID, sessionID, onNavigate }: ConversationSidebarProps) {
  const navigate = useNavigate()
  const client = useQueryClient()
  const [showArchived, setShowArchived] = useState(false)
  const agent = useQuery({ queryKey: ['agent', agentID], queryFn: () => api.agent(agentID), enabled: Number.isFinite(agentID) })
  const sessions = useQuery({ queryKey: ['sessions', agentID], queryFn: () => api.sessions(agentID), enabled: Number.isFinite(agentID) })
  const archived = useQuery({ queryKey: ['sessions', agentID, 'archived'], queryFn: () => api.sessions(agentID, true), enabled: Number.isFinite(agentID) && showArchived })
  const create = useMutation({
    mutationFn: () => api.createSession(agentID),
    onSuccess: item => {
      client.invalidateQueries({ queryKey: ['sessions', agentID] })
      client.invalidateQueries({ queryKey: ['recent-sessions'] })
      navigate(`/agents/${agentID}/sessions/${item.id}`)
      onNavigate?.()
    },
  })
  const restore = useMutation({
    mutationFn: (id: number) => api.archiveSession(id, true),
    onSuccess: (_, id) => {
      client.invalidateQueries({ queryKey: ['sessions', agentID] })
      client.invalidateQueries({ queryKey: ['sessions', agentID, 'archived'] })
      client.invalidateQueries({ queryKey: ['recent-sessions'] })
      navigate(`/agents/${agentID}/sessions/${id}`)
      setShowArchived(false)
      onNavigate?.()
    },
  })

  return <div className="conversation-sidebar-content">
    <div className="session-panel-head">
      <div><div className="eyebrow">Conversations</div><h2>{agent.data?.name || 'Agent'}</h2></div>
      <Link to={`/agents/${agentID}/edit`} className="small-icon-button" aria-label="Agent settings" onClick={onNavigate}><Settings2 /></Link>
      <button className="small-icon-button" onClick={() => create.mutate()} disabled={create.isPending} aria-label="New conversation"><MessageSquarePlus /></button>
    </div>
    <div className="session-list">{sessions.isLoading ? [1, 2, 3].map(i => <div className="session-row skeleton" key={i} />) : sessions.data?.sessions.length ? sessions.data.sessions.map(item => <Link key={item.id} to={`/agents/${agentID}/sessions/${item.id}`} className={`session-row ${item.id === sessionID ? 'active' : ''}`} onClick={onNavigate}><span className="session-icon"><Bot size={15} /></span><div><b><InlineMarkdown>{item.title}</InlineMarkdown></b><span>{formatRelative(item.updated_at)}</span></div></Link>) : <div className="panel-empty"><MessageSquarePlus /><p>No conversations yet.</p><button className="button small secondary" onClick={() => create.mutate()} disabled={create.isPending}>Start one</button></div>}</div>
    <div className="archived-session-block">
      <button className="archived-session-toggle" onClick={() => setShowArchived(value => !value)} aria-expanded={showArchived}><Archive size={14} /> Archived conversations <ChevronDown className={showArchived ? 'open' : ''} size={14} /></button>
      {showArchived && <div className="archived-session-list">{archived.isLoading ? <div className="session-row skeleton" /> : archived.data?.sessions.length ? archived.data.sessions.map(item => <div className="archived-session-row" key={item.id}><div><b><InlineMarkdown>{item.title}</InlineMarkdown></b><span>{formatRelative(item.updated_at)}</span></div><button onClick={() => restore.mutate(item.id)} disabled={restore.isPending} aria-label={`Restore ${item.title}`}><RotateCcw /></button></div>) : <p>No archived conversations.</p>}</div>}
    </div>
  </div>
}

function formatRelative(value: string) {
  const diff = Date.now() - new Date(value).getTime()
  if (diff < 60_000) return 'Just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' }).format(new Date(value))
}
