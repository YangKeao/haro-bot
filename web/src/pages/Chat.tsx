import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, LoaderCircle, MoreHorizontal, Paperclip, Pencil, Plus, Send, Sparkles, Square, X } from 'lucide-react'
import { useNavigate, useParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { api, APIError, streamRun } from '../api'
import type { AgentProfile, Attachment, Message, RunEvent, TraceStep } from '../types'
import { AgentAvatar } from './Home'
import MarkdownEditor from '../components/MarkdownEditor'
import ProcessPanel from '../components/ProcessPanel'
import InlineMarkdown from '../components/InlineMarkdown'
import TraceSidebar from '../components/TraceSidebar'
import { buildConversationTurns, emptyLiveRun, reduceRunEvent } from '../trace'
import { formatMessageTime, formatMessageTimeTitle } from '../time'

function MessageBubble({ message, agent, trace = [], running = false, onOpenTrace }: { message: Message; agent?: AgentProfile; trace?: TraceStep[]; running?: boolean; onOpenTrace?: () => void }) {
  const meta = message.metadata || {}
  const timestamp = message.id === 'stream' ? '' : formatMessageTime(message.created_at)
  if (message.role === 'tool') return null
  if (message.role === 'assistant' && !message.content && !message.attachments?.length && !trace.length && !running && (!meta.status || meta.status === 'ok')) return null
  return <article className={`message ${message.role}`}>
		<div className="message-avatar">{message.role === 'assistant' && agent ? <AgentAvatar agent={agent} /> : message.role === 'assistant' ? <Sparkles size={16} /> : 'You'}</div>
    <div className="message-body">
      {message.attachments?.length ? <div className="message-images">{message.attachments.map(image => <a key={image.id} href={`/api/v1/attachments/${image.id}`} target="_blank" rel="noreferrer"><img src={`/api/v1/attachments/${image.id}`} alt={image.name} /></a>)}</div> : null}
      {message.role === 'assistant' && (running || trace.length > 0) && <button className={`thinking-indicator ${running ? 'running' : ''} ${meta.status === 'error' || meta.status === 'cancelled' ? 'interrupted' : ''}`} onClick={onOpenTrace}>
        <span className="thinking-orbit"><Sparkles /></span><span>{running ? 'Thinking…' : meta.status === 'error' || meta.status === 'cancelled' ? 'Thinking interrupted' : `View thinking · ${trace.length} step${trace.length === 1 ? '' : 's'}`}</span>
      </button>}
      {message.content && <div className="markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>{message.content}</ReactMarkdown></div>}
      {meta.status && meta.status !== 'ok' && <span className={`message-status ${meta.status}`}>{meta.status}</span>}
      {timestamp && <time className="message-time" dateTime={message.created_at} title={formatMessageTimeTitle(message.created_at)}>{timestamp}</time>}
    </div>
  </article>
}

export default function Chat() {
  const { agentID, sessionID } = useParams()
  const activeAgentID = Number(agentID)
  const activeSessionID = sessionID ? Number(sessionID) : undefined
  const navigate = useNavigate()
  const client = useQueryClient()
  const [draft, setDraft] = useState('')
  const [pending, setPending] = useState<Attachment[]>([])
  const [liveRun, setLiveRun] = useState(emptyLiveRun)
  const [tracePanel, setTracePanel] = useState<string>()
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string>()
  const [renaming, setRenaming] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const abortRef = useRef<AbortController | undefined>(undefined)
  const bottomRef = useRef<HTMLDivElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const agent = useQuery({ queryKey: ['agent', activeAgentID], queryFn: () => api.agent(activeAgentID), enabled: Number.isFinite(activeAgentID) })
  const sessions = useQuery({ queryKey: ['sessions', activeAgentID], queryFn: () => api.sessions(activeAgentID), enabled: Number.isFinite(activeAgentID) })
  const session = useQuery({ queryKey: ['session', activeSessionID], queryFn: () => api.session(activeSessionID!), enabled: !!activeSessionID })
  const messages = useQuery({ queryKey: ['messages', activeSessionID], queryFn: () => api.messages(activeSessionID!), enabled: !!activeSessionID })
  const conversationTurns = useMemo(() => buildConversationTurns(messages.data?.messages), [messages.data?.messages])
  const selectedTurn = tracePanel === 'latest' ? conversationTurns.at(-1) : conversationTurns.find(turn => `turn-${turn.id}` === tracePanel)
  const selectedTrace = tracePanel === 'live' ? liveRun.trace : selectedTurn?.trace
  const selectedStatus = tracePanel === 'live' ? undefined : selectedTurn?.status

  useEffect(() => {
    if (!activeSessionID && sessions.data?.sessions.length) navigate(`/agents/${activeAgentID}/sessions/${sessions.data.sessions[0].id}`, { replace: true })
  }, [activeAgentID, activeSessionID, navigate, sessions.data])
  useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: running ? 'smooth' : 'auto' }) }, [messages.data, liveRun, running])
  useEffect(() => { setPending([]); setDraft(''); setError(undefined); setLiveRun(emptyLiveRun); setTracePanel(undefined) }, [activeSessionID])
  useEffect(() => {
    if (!tracePanel) return
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape') setTracePanel(undefined) }
    window.addEventListener('keydown', close)
    return () => window.removeEventListener('keydown', close)
  }, [tracePanel])

  const createSession = useMutation({ mutationFn: () => api.createSession(activeAgentID), onSuccess: item => { client.invalidateQueries({ queryKey: ['sessions', activeAgentID] }); client.invalidateQueries({ queryKey: ['recent-sessions'] }); navigate(`/agents/${activeAgentID}/sessions/${item.id}`) } })
  const rename = useMutation({ mutationFn: () => api.renameSession(activeSessionID!, renameValue.trim()), onSuccess: () => { setRenaming(false); client.invalidateQueries({ queryKey: ['sessions', activeAgentID] }); client.invalidateQueries({ queryKey: ['session', activeSessionID] }); client.invalidateQueries({ queryKey: ['recent-sessions'] }) } })
  const archive = useMutation({ mutationFn: () => api.archiveSession(activeSessionID!), onSuccess: () => { client.invalidateQueries({ queryKey: ['sessions', activeAgentID] }); client.invalidateQueries({ queryKey: ['recent-sessions'] }); navigate(`/agents/${activeAgentID}/sessions`) } })
  const upload = useMutation({ mutationFn: (file: File) => api.upload(activeSessionID!, file), onSuccess: image => setPending(items => [...items, image]), onError: e => setError(e instanceof APIError ? e.message : 'Image upload failed') })

  const chooseImages = (event: ChangeEvent<HTMLInputElement>) => {
    const files = [...(event.target.files || [])]
    if (pending.length + files.length > 4) { setError('A message can contain at most four images.'); return }
    files.forEach(file => upload.mutate(file))
    event.target.value = ''
  }
  const removePending = async (image: Attachment) => {
    await api.deleteAttachment(image.id)
    setPending(items => items.filter(item => item.id !== image.id))
  }
  const onRunEvent = (event: RunEvent) => {
    setLiveRun(current => reduceRunEvent(current, event))
    if (event.event === 'run.failed') setError(String(event.data.message || 'The run failed.'))
  }
  const send = async () => {
    if (!activeSessionID || running || (!draft.trim() && !pending.length)) return
    const content = draft.trim()
    const images = pending
    setDraft(''); setPending([]); setError(undefined); setLiveRun(emptyLiveRun); setRunning(true)
    client.setQueryData<{ messages: Message[] }>(['messages', activeSessionID], old => ({ ...(old || { messages: [] }), messages: [...(old?.messages || []), { id: `optimistic-${Date.now()}`, session_id: activeSessionID, role: 'user', content, attachments: images, created_at: new Date().toISOString() }] }))
    const controller = new AbortController(); abortRef.current = controller
    try { await streamRun(activeSessionID, content, images.map(image => image.id), onRunEvent, controller.signal) }
    catch (e) { if (!(e instanceof DOMException && e.name === 'AbortError')) setError(e instanceof APIError ? e.message : 'The connection was interrupted.') }
    finally {
      setRunning(false); abortRef.current = undefined
		await Promise.all([client.invalidateQueries({ queryKey: ['messages', activeSessionID] }), client.invalidateQueries({ queryKey: ['sessions', activeAgentID] }), client.invalidateQueries({ queryKey: ['session', activeSessionID] }), client.invalidateQueries({ queryKey: ['agent', activeAgentID] }), client.invalidateQueries({ queryKey: ['agents'] }), client.invalidateQueries({ queryKey: ['recent-sessions'] })])
      setTracePanel(current => current === 'live' ? 'latest' : current)
      setLiveRun(emptyLiveRun)
    }
  }
  const stop = async () => { if (!activeSessionID) return; await api.cancel(activeSessionID).catch(() => undefined); abortRef.current?.abort() }
	return <main className={`workspace-page ${tracePanel ? 'trace-open' : ''}`}>
    <section className="chat-panel">
      <header className="chat-header"><span className="chat-header-spacer" aria-hidden="true" />
        <div className="chat-title">{renaming ? <form onSubmit={e => { e.preventDefault(); rename.mutate() }}><input autoFocus value={renameValue} onChange={e => setRenameValue(e.target.value)} onBlur={() => setRenaming(false)} /></form> : <><h1>{session.data?.session.title ? <InlineMarkdown>{session.data.session.title}</InlineMarkdown> : activeSessionID ? 'Loading…' : 'New conversation'}</h1><span>{agent.data?.model}</span></>}</div>
        {activeSessionID && <DropdownMenu.Root><DropdownMenu.Trigger asChild><button className="icon-button" aria-label="Conversation actions"><MoreHorizontal /></button></DropdownMenu.Trigger><DropdownMenu.Portal><DropdownMenu.Content className="dropdown" align="end"><DropdownMenu.Item onSelect={() => { setRenameValue(session.data?.session.title || ''); setRenaming(true) }}><Pencil size={15} /> Rename</DropdownMenu.Item><DropdownMenu.Item className="danger" disabled={running} onSelect={() => archive.mutate()}><Archive size={15} /> Archive</DropdownMenu.Item></DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root>}
      </header>
      {!activeSessionID ? <div className="chat-empty"><div className="empty-orbit"><Sparkles /></div><h2>Start a fresh conversation</h2><p>Open a new session with {agent.data?.name || 'this agent'}.</p><button className="button primary" onClick={() => createSession.mutate()}><Plus size={16} /> New session</button></div> : <>
        <div className="message-scroll"><div className="message-column">
		  {messages.isLoading ? <div className="conversation-skeleton"><div /><div /><div /></div> : !messages.data?.messages.length && !running ? <div className="chat-welcome">{agent.data && <AgentAvatar agent={agent.data} size="large" />}<h2>Talk to {agent.data?.name}</h2><p>{agent.data?.description || 'Send a message or attach an image to get started.'}</p></div> : conversationTurns.map(turn => <div className="conversation-turn" key={turn.id}>{turn.user && <MessageBubble message={turn.user} agent={agent.data} />}{turn.assistant && <MessageBubble message={turn.assistant} agent={agent.data} trace={turn.trace} onOpenTrace={() => setTracePanel(`turn-${turn.id}`)} />}</div>)}
		  {(running || liveRun.answer || liveRun.trace.length > 0) && <MessageBubble message={{ ...streamSeed(activeSessionID), content: liveRun.answer }} agent={agent.data} trace={liveRun.trace} running={running} onOpenTrace={() => setTracePanel('live')} />}
          {error && <div className="run-error"><span>Run interrupted</span><p>{error}</p></div>}
          <div ref={bottomRef} />
        </div></div>
        <ProcessPanel sessionID={activeSessionID} enabled={Boolean(agent.data?.sandbox_id)} />
        <footer className="composer-wrap"><div className="composer-column">
          {pending.length > 0 && <div className="pending-images">{pending.map(image => <div key={image.id}><img src={`/api/v1/attachments/${image.id}`} alt={image.name} /><button onClick={() => void removePending(image)} aria-label={`Remove ${image.name}`}><X /></button></div>)}</div>}
          {upload.isPending && <div className="uploading"><LoaderCircle className="spin" /> Uploading image…</div>}
		  <div className={`composer ${running ? 'running' : ''}`}><MarkdownEditor variant="composer" value={draft} onChange={setDraft} onSubmit={() => void send()} disabled={running} placeholder={running ? `${agent.data?.name || 'Agent'} is working…` : `Message ${agent.data?.name || 'agent'}…`} ariaLabel="Message" />
			<div className="composer-actions"><input ref={fileRef} type="file" accept="image/jpeg,image/png,image/webp" multiple hidden onChange={chooseImages} /><button className="composer-icon" disabled={running || pending.length >= 4} onClick={() => fileRef.current?.click()} aria-label="Attach images"><Paperclip /></button><span className="composer-hint">⌘/Ctrl + Enter to send</span>{running ? <button className="send-button stop" onClick={() => void stop()} aria-label="Stop generating"><Square /></button> : <button className="send-button" disabled={!draft.trim() && !pending.length} onClick={() => void send()} aria-label="Send message"><Send /></button>}</div>
          </div><div className="composer-note">Haro can make mistakes. Review tool output before relying on it.</div>
        </div></footer>
      </>}
    </section>
    {tracePanel && <TraceSidebar steps={selectedTrace || []} running={tracePanel === 'live' && running} status={selectedStatus} onClose={() => setTracePanel(undefined)} />}
  </main>
}

function streamSeed(sessionID: number): Message { return { id: 'stream', session_id: sessionID, role: 'assistant', content: '', metadata: {}, created_at: new Date().toISOString() } }
