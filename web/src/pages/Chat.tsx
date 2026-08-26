import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, Download, FileArchive, FileIcon, LoaderCircle, MoreHorizontal, Paperclip, Pencil, Plus, Send, Sparkles, Square, X } from 'lucide-react'
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

interface UploadItem {
  id: string
  file: File
  progress: number
}

function isPreviewAttachment(attachment: Pick<Attachment, 'mime_type'>) {
  return ['image/jpeg', 'image/png', 'image/webp', 'image/gif'].includes(attachment.mime_type.toLowerCase().split(';', 1)[0])
}

function isArchiveName(name: string) {
  return /\.(zip|tar|tgz|gz|bz2|xz|7z|rar)$/i.test(name)
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB']
  let value = bytes / 1024
  let unit = units[0]
  for (let i = 1; i < units.length && value >= 1024; i += 1) { value /= 1024; unit = units[i] }
  return `${value >= 10 ? value.toFixed(0) : value.toFixed(1)} ${unit}`
}

export function AttachmentLink({ attachment, removable, onRemove }: { attachment: Attachment; removable?: boolean; onRemove?: () => void }) {
  const preview = isPreviewAttachment(attachment)
  const url = `/api/v1/attachments/${attachment.id}`
  const download = <a className="attachment-download" href={`${url}?download=1`} download={attachment.name} aria-label={`Download ${attachment.name}`} title={`Download ${attachment.name}`}><Download /><span>Download</span></a>
  if (preview) {
    return <div className="attachment-item preview">
      <a className="attachment-preview-link" href={url} target="_blank" rel="noreferrer" aria-label={`Open ${attachment.name}`}><img src={url} alt={attachment.name} loading="lazy" /></a>
      {!removable && <div className="attachment-footer"><span className="attachment-copy"><strong>{attachment.name}</strong><small>{formatBytes(attachment.size_bytes)}</small></span>{download}</div>}
      {removable && <button className="attachment-remove" onClick={onRemove} aria-label={`Remove ${attachment.name}`}><X /></button>}
    </div>
  }
  return <div className="attachment-item file">
    <span className="attachment-icon">{isArchiveName(attachment.name) ? <FileArchive /> : <FileIcon />}</span>
    <span className="attachment-copy"><strong>{attachment.name}</strong><small>{attachment.mime_type || 'File'} · {formatBytes(attachment.size_bytes)}</small></span>
    {!removable && download}
    {removable && <button className="attachment-remove" onClick={onRemove} aria-label={`Remove ${attachment.name}`}><X /></button>}
  </div>
}

function MessageBubble({ message, agent, trace = [], running = false, onOpenTrace }: { message: Message; agent?: AgentProfile; trace?: TraceStep[]; running?: boolean; onOpenTrace?: () => void }) {
  const meta = message.metadata || {}
  const timestamp = message.id === 'stream' ? '' : formatMessageTime(message.created_at)
  if (message.role === 'tool') return null
  if (message.role === 'assistant' && !message.content && !message.attachments?.length && !trace.length && !running && (!meta.status || meta.status === 'ok')) return null
  return <article className={`message ${message.role}`}>
		<div className="message-avatar">{message.role === 'assistant' && agent ? <AgentAvatar agent={agent} /> : message.role === 'assistant' ? <Sparkles size={16} /> : 'You'}</div>
    <div className="message-body">
      {message.attachments?.length ? <div className="message-attachments">{message.attachments.map(attachment => <AttachmentLink key={attachment.id} attachment={attachment} />)}</div> : null}
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
	const [uploads, setUploads] = useState<UploadItem[]>([])
  const [liveRun, setLiveRun] = useState(emptyLiveRun)
  const [tracePanel, setTracePanel] = useState<string>()
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string>()
  const [renaming, setRenaming] = useState(false)
  const [renameValue, setRenameValue] = useState('')
  const abortRef = useRef<AbortController | undefined>(undefined)
  const bottomRef = useRef<HTMLDivElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
	const uploadQueueRef = useRef<Promise<void>>(Promise.resolve())
	const uploadControllersRef = useRef(new Map<string, AbortController>())
	const cancelledUploadsRef = useRef(new Set<string>())
	const uploadGenerationRef = useRef(0)

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
  useEffect(() => {
		uploadGenerationRef.current += 1
		uploadControllersRef.current.forEach(controller => controller.abort())
		uploadControllersRef.current.clear()
		cancelledUploadsRef.current.clear()
		setUploads([]); setPending([]); setDraft(''); setError(undefined); setLiveRun(emptyLiveRun); setTracePanel(undefined)
	}, [activeSessionID])
  useEffect(() => {
    if (!tracePanel) return
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape') setTracePanel(undefined) }
    window.addEventListener('keydown', close)
    return () => window.removeEventListener('keydown', close)
  }, [tracePanel])

  const createSession = useMutation({ mutationFn: () => api.createSession(activeAgentID), onSuccess: item => { client.invalidateQueries({ queryKey: ['sessions', activeAgentID] }); client.invalidateQueries({ queryKey: ['recent-sessions'] }); navigate(`/agents/${activeAgentID}/sessions/${item.id}`) } })
  const rename = useMutation({ mutationFn: () => api.renameSession(activeSessionID!, renameValue.trim()), onSuccess: () => { setRenaming(false); client.invalidateQueries({ queryKey: ['sessions', activeAgentID] }); client.invalidateQueries({ queryKey: ['session', activeSessionID] }); client.invalidateQueries({ queryKey: ['recent-sessions'] }) } })
  const archive = useMutation({ mutationFn: () => api.archiveSession(activeSessionID!), onSuccess: () => { client.invalidateQueries({ queryKey: ['sessions', activeAgentID] }); client.invalidateQueries({ queryKey: ['recent-sessions'] }); navigate(`/agents/${activeAgentID}/sessions`) } })
  const chooseFiles = (event: ChangeEvent<HTMLInputElement>) => {
    const files = [...(event.target.files || [])]
		const generation = uploadGenerationRef.current
		const sessionID = activeSessionID
		if (!sessionID) return
		const queued = files.map((file, index) => ({ id: `${Date.now()}-${index}-${Math.random().toString(36).slice(2)}`, file, progress: 0 }))
		setUploads(items => [...items, ...queued])
		for (const item of queued) {
			uploadQueueRef.current = uploadQueueRef.current.then(async () => {
				if (generation !== uploadGenerationRef.current || cancelledUploadsRef.current.has(item.id)) return
				const controller = new AbortController()
				uploadControllersRef.current.set(item.id, controller)
				try {
					const attachment = await api.upload(sessionID, item.file, progress => setUploads(items => items.map(current => current.id === item.id ? { ...current, progress } : current)), controller.signal)
					if (generation === uploadGenerationRef.current && !cancelledUploadsRef.current.has(item.id)) setPending(items => [...items, attachment])
				} catch (e) {
					if (!(e instanceof DOMException && e.name === 'AbortError') && generation === uploadGenerationRef.current) setError(e instanceof APIError ? e.message : 'File upload failed')
				} finally {
					uploadControllersRef.current.delete(item.id)
					setUploads(items => items.filter(current => current.id !== item.id))
				}
			})
		}
    event.target.value = ''
  }
	const cancelUpload = (id: string) => {
		cancelledUploadsRef.current.add(id)
		uploadControllersRef.current.get(id)?.abort()
		setUploads(items => items.filter(item => item.id !== id))
	}
  const removePending = async (attachment: Attachment) => {
    await api.deleteAttachment(attachment.id)
    setPending(items => items.filter(item => item.id !== attachment.id))
  }
  const onRunEvent = (event: RunEvent) => {
    setLiveRun(current => reduceRunEvent(current, event))
    if (event.event === 'run.failed') setError(String(event.data.message || 'The run failed.'))
  }
  const send = async () => {
		if (!activeSessionID || running || uploads.length > 0 || (!draft.trim() && !pending.length)) return
    const content = draft.trim()
		const attachments = pending
    setDraft(''); setPending([]); setError(undefined); setLiveRun(emptyLiveRun); setRunning(true)
		client.setQueryData<{ messages: Message[] }>(['messages', activeSessionID], old => ({ ...(old || { messages: [] }), messages: [...(old?.messages || []), { id: `optimistic-${Date.now()}`, session_id: activeSessionID, role: 'user', content, attachments, created_at: new Date().toISOString() }] }))
    const controller = new AbortController(); abortRef.current = controller
		try { await streamRun(activeSessionID, content, attachments.map(attachment => attachment.id), onRunEvent, controller.signal) }
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
		  {messages.isLoading ? <div className="conversation-skeleton"><div /><div /><div /></div> : !messages.data?.messages.length && !running ? <div className="chat-welcome">{agent.data && <AgentAvatar agent={agent.data} size="large" />}<h2>Talk to {agent.data?.name}</h2><p>{agent.data?.description || 'Send a message or attach a file to get started.'}</p></div> : conversationTurns.map(turn => <div className="conversation-turn" key={turn.id}>{turn.user && <MessageBubble message={turn.user} agent={agent.data} />}{turn.assistant && <MessageBubble message={turn.assistant} agent={agent.data} trace={turn.trace} onOpenTrace={() => setTracePanel(`turn-${turn.id}`)} />}</div>)}
		  {(running || liveRun.answer || liveRun.trace.length > 0 || liveRun.attachments.length > 0) && <MessageBubble message={{ ...streamSeed(activeSessionID), content: liveRun.answer, attachments: liveRun.attachments }} agent={agent.data} trace={liveRun.trace} running={running} onOpenTrace={() => setTracePanel('live')} />}
          {error && <div className="run-error"><span>Run interrupted</span><p>{error}</p></div>}
          <div ref={bottomRef} />
        </div></div>
        <ProcessPanel sessionID={activeSessionID} enabled={Boolean(agent.data?.sandbox_id)} />
        <footer className="composer-wrap"><div className="composer-column">
			{(pending.length > 0 || uploads.length > 0) && <div className="pending-attachments">
				{pending.map(attachment => <AttachmentLink key={attachment.id} attachment={attachment} removable onRemove={() => void removePending(attachment)} />)}
				{uploads.map(item => <div className="attachment-item uploading-file" key={item.id}><span className="attachment-icon">{isArchiveName(item.file.name) ? <FileArchive /> : <LoaderCircle className="spin" />}</span><span className="attachment-copy"><strong>{item.file.name}</strong><small>{item.progress > 0 ? `${item.progress}% · ` : ''}{formatBytes(item.file.size)}</small><span className="upload-progress"><i style={{ width: `${item.progress}%` }} /></span></span><button onClick={() => cancelUpload(item.id)} aria-label={`Cancel ${item.file.name}`}><X /></button></div>)}
			</div>}
		  <div className={`composer ${running ? 'running' : ''}`}><MarkdownEditor variant="composer" value={draft} onChange={setDraft} onSubmit={() => void send()} disabled={running} placeholder={running ? `${agent.data?.name || 'Agent'} is working…` : `Message ${agent.data?.name || 'agent'}…`} ariaLabel="Message" />
			<div className="composer-actions"><input ref={fileRef} type="file" multiple hidden onChange={chooseFiles} /><button className="composer-icon" disabled={running} onClick={() => fileRef.current?.click()} aria-label="Attach files"><Paperclip /></button><span className="composer-hint">⌘/Ctrl + Enter to send</span>{running ? <button className="send-button stop" onClick={() => void stop()} aria-label="Stop generating"><Square /></button> : <button className="send-button" disabled={uploads.length > 0 || (!draft.trim() && !pending.length)} onClick={() => void send()} aria-label="Send message"><Send /></button>}</div>
          </div><div className="composer-note">Haro can make mistakes. Review tool output before relying on it.</div>
        </div></footer>
      </>}
    </section>
    {tracePanel && <TraceSidebar steps={selectedTrace || []} running={tracePanel === 'live' && running} status={selectedStatus} onClose={() => setTracePanel(undefined)} />}
  </main>
}

function streamSeed(sessionID: number): Message { return { id: 'stream', session_id: sessionID, role: 'assistant', content: '', metadata: {}, created_at: new Date().toISOString() } }
