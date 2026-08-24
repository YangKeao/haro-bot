import { useEffect, useMemo, useRef, useState, type ChangeEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, Check, ChevronDown, LoaderCircle, MoreHorizontal, Paperclip, Pencil, Plus, Send, Sparkles, Square, X } from 'lucide-react'
import { useNavigate, useParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { api, APIError, streamRun } from '../api'
import type { AgentProfile, Attachment, Message, RunEvent, ToolCall } from '../types'
import { AgentAvatar } from './Home'
import MarkdownEditor from '../components/MarkdownEditor'
import ProcessEntry from '../components/ProcessEntry'
import ProcessPanel from '../components/ProcessPanel'
import InlineMarkdown from '../components/InlineMarkdown'
import { aggregateProcessTools, type DisplayProcess } from '../processes'

type ToolActivity = { id: string; name: string; arguments?: unknown; content?: string; done?: boolean; truncated?: boolean }

function MessageBubble({ message, agent, inlineProcesses, hiddenToolCalls, hiddenToolResults }: { message: Message; agent?: AgentProfile; inlineProcesses?: Map<string, DisplayProcess>; hiddenToolCalls?: Set<string>; hiddenToolResults?: Set<string> }) {
  const meta = message.metadata || {}
  const process = meta.tool_call_id ? inlineProcesses?.get(meta.tool_call_id) : undefined
  if (message.role === 'tool' && process) return <div className="inline-process"><ProcessEntry process={process} className="process-inline" /></div>
  if (message.role === 'tool' && meta.tool_call_id && hiddenToolResults?.has(meta.tool_call_id)) return null
  if (message.role === 'tool') return <details className="tool-result"><summary><span className={`status-dot ${meta.status === 'error' ? 'error' : ''}`} /> {meta.mcp_server ? `${meta.mcp_server} · ${meta.tool_name || 'tool'}` : meta.tool_name || 'Tool result'} <ChevronDown size={14} /></summary><div className="tool-detail">{message.attachments?.length ? <div className="message-images">{message.attachments.map(image => <a key={image.id} href={`/api/v1/attachments/${image.id}`} target="_blank" rel="noreferrer"><img src={`/api/v1/attachments/${image.id}`} alt={image.name} /></a>)}</div> : null}<pre>{meta.display_content || message.content}</pre>{meta.structured_content !== undefined && <details><summary>Structured result</summary><pre>{JSON.stringify(meta.structured_content, null, 2)}</pre></details>}</div></details>
  const toolCalls = meta.tool_calls?.filter(call => !hiddenToolCalls?.has(call.id))
  if (message.role === 'assistant' && !message.content && !message.attachments?.length && !meta.reasoning_content && !toolCalls?.length && (!meta.status || meta.status === 'ok')) return null
  return <article className={`message ${message.role}`}>
		<div className="message-avatar">{message.role === 'assistant' && agent ? <AgentAvatar agent={agent} /> : message.role === 'assistant' ? <Sparkles size={16} /> : 'You'}</div>
    <div className="message-body">
      {message.attachments?.length ? <div className="message-images">{message.attachments.map(image => <a key={image.id} href={`/api/v1/attachments/${image.id}`} target="_blank" rel="noreferrer"><img src={`/api/v1/attachments/${image.id}`} alt={image.name} /></a>)}</div> : null}
      {meta.reasoning_content && <details className="reasoning"><summary><Sparkles size={14} /> Reasoning <ChevronDown size={14} /></summary><div>{meta.reasoning_content}</div></details>}
      {message.content && <div className="markdown"><ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>{message.content}</ReactMarkdown></div>}
      {toolCalls?.map(call => <details className="tool-call" key={call.id}><summary><span className="status-dot" /> {call.function.name}<ChevronDown size={14} /></summary><pre>{prettyJSON(call.function.arguments)}</pre></details>)}
      {meta.status && meta.status !== 'ok' && <span className={`message-status ${meta.status}`}>{meta.status}</span>}
    </div>
  </article>
}

function prettyJSON(value: string) {
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}

function ToolTimeline({ tools }: { tools: ToolActivity[] }) {
  if (!tools.length) return null
	const grouped = aggregateProcessTools(tools)
  return <div className="live-tools">{tools.map(tool => {
    const process = grouped.processes.get(tool.id)
    if (process) return <ProcessEntry key={tool.id} process={process} className="process-inline" />
    if (grouped.hiddenCallIDs.has(tool.id)) return null
    return <details key={tool.id} className="tool-call" open={!tool.done}><summary>{tool.done ? <Check size={14} /> : <LoaderCircle className="spin" size={14} />} {tool.name}<ChevronDown size={14} /></summary><div className="tool-detail"><b>Arguments</b><pre>{typeof tool.arguments === 'string' ? tool.arguments : JSON.stringify(tool.arguments, null, 2)}</pre>{tool.done && <><b>Result{tool.truncated ? ' (preview)' : ''}</b><pre>{tool.content}</pre></>}</div></details>
  })}</div>
}

function messageToolContext(messages: Message[] = []) {
  const calls = new Map<string, ToolCall>()
  for (const message of messages) for (const call of message.metadata?.tool_calls || []) calls.set(call.id, call)
	const activities: ToolActivity[] = []
  for (const message of messages) {
    const id = message.metadata?.tool_call_id
    const call = id ? calls.get(id) : undefined
		if (id && call) activities.push({ id, name: call.function.name, arguments: call.function.arguments, content: message.content, done: true })
  }
	const grouped = aggregateProcessTools(activities)
  return { processes: grouped.processes, hiddenToolCalls: grouped.hiddenCallIDs, hiddenToolResults: grouped.hiddenResultIDs }
}

export default function Chat() {
  const { agentID, sessionID } = useParams()
  const activeAgentID = Number(agentID)
  const activeSessionID = sessionID ? Number(sessionID) : undefined
  const navigate = useNavigate()
  const client = useQueryClient()
  const [draft, setDraft] = useState('')
  const [pending, setPending] = useState<Attachment[]>([])
  const [streamMessage, setStreamMessage] = useState<Message>()
  const [tools, setTools] = useState<ToolActivity[]>([])
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
  const messageTools = useMemo(() => messageToolContext(messages.data?.messages), [messages.data?.messages])

  useEffect(() => {
    if (!activeSessionID && sessions.data?.sessions.length) navigate(`/agents/${activeAgentID}/sessions/${sessions.data.sessions[0].id}`, { replace: true })
  }, [activeAgentID, activeSessionID, navigate, sessions.data])
  useEffect(() => { bottomRef.current?.scrollIntoView({ behavior: running ? 'smooth' : 'auto' }) }, [messages.data, streamMessage?.content, tools, running])
  useEffect(() => { setPending([]); setDraft(''); setError(undefined); setTools([]); setStreamMessage(undefined) }, [activeSessionID])

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
    if (event.event === 'assistant.delta') setStreamMessage(current => ({ ...(current || streamSeed(activeSessionID!)), content: (current?.content || '') + String(event.data.delta || '') }))
    if (event.event === 'reasoning.delta') setStreamMessage(current => ({ ...(current || streamSeed(activeSessionID!)), metadata: { ...(current?.metadata || {}), reasoning_content: (current?.metadata?.reasoning_content || '') + String(event.data.delta || '') } }))
    if (event.event === 'tool.started') setTools(current => [...current, { id: String(event.data.id), name: String(event.data.name), arguments: event.data.arguments }])
    if (event.event === 'tool.completed') setTools(current => current.map(tool => tool.id === String(event.data.id) ? { ...tool, done: true, content: String(event.data.content || ''), truncated: Boolean(event.data.truncated) } : tool))
    if (event.event === 'run.failed') setError(String(event.data.message || 'The run failed.'))
  }
  const send = async () => {
    if (!activeSessionID || running || (!draft.trim() && !pending.length)) return
    const content = draft.trim()
    const images = pending
    setDraft(''); setPending([]); setError(undefined); setTools([]); setStreamMessage(streamSeed(activeSessionID)); setRunning(true)
    client.setQueryData<{ messages: Message[] }>(['messages', activeSessionID], old => ({ ...(old || { messages: [] }), messages: [...(old?.messages || []), { id: `optimistic-${Date.now()}`, session_id: activeSessionID, role: 'user', content, attachments: images, created_at: new Date().toISOString() }] }))
    const controller = new AbortController(); abortRef.current = controller
    try { await streamRun(activeSessionID, content, images.map(image => image.id), onRunEvent, controller.signal) }
    catch (e) { if (!(e instanceof DOMException && e.name === 'AbortError')) setError(e instanceof APIError ? e.message : 'The connection was interrupted.') }
    finally {
      setRunning(false); abortRef.current = undefined
		await Promise.all([client.invalidateQueries({ queryKey: ['messages', activeSessionID] }), client.invalidateQueries({ queryKey: ['sessions', activeAgentID] }), client.invalidateQueries({ queryKey: ['session', activeSessionID] }), client.invalidateQueries({ queryKey: ['agent', activeAgentID] }), client.invalidateQueries({ queryKey: ['agents'] }), client.invalidateQueries({ queryKey: ['recent-sessions'] })])
      setStreamMessage(undefined); setTools([])
    }
  }
  const stop = async () => { if (!activeSessionID) return; await api.cancel(activeSessionID).catch(() => undefined); abortRef.current?.abort() }
	return <main className="workspace-page">
    <section className="chat-panel">
      <header className="chat-header"><span className="chat-header-spacer" aria-hidden="true" />
        <div className="chat-title">{renaming ? <form onSubmit={e => { e.preventDefault(); rename.mutate() }}><input autoFocus value={renameValue} onChange={e => setRenameValue(e.target.value)} onBlur={() => setRenaming(false)} /></form> : <><h1>{session.data?.session.title ? <InlineMarkdown>{session.data.session.title}</InlineMarkdown> : activeSessionID ? 'Loading…' : 'New conversation'}</h1><span>{agent.data?.model}</span></>}</div>
        {activeSessionID && <DropdownMenu.Root><DropdownMenu.Trigger asChild><button className="icon-button" aria-label="Conversation actions"><MoreHorizontal /></button></DropdownMenu.Trigger><DropdownMenu.Portal><DropdownMenu.Content className="dropdown" align="end"><DropdownMenu.Item onSelect={() => { setRenameValue(session.data?.session.title || ''); setRenaming(true) }}><Pencil size={15} /> Rename</DropdownMenu.Item><DropdownMenu.Item className="danger" disabled={running} onSelect={() => archive.mutate()}><Archive size={15} /> Archive</DropdownMenu.Item></DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root>}
      </header>
      {!activeSessionID ? <div className="chat-empty"><div className="empty-orbit"><Sparkles /></div><h2>Start a fresh conversation</h2><p>Open a new session with {agent.data?.name || 'this agent'}.</p><button className="button primary" onClick={() => createSession.mutate()}><Plus size={16} /> New session</button></div> : <>
        <div className="message-scroll"><div className="message-column">
		  {messages.isLoading ? <div className="conversation-skeleton"><div /><div /><div /></div> : !messages.data?.messages.length && !running ? <div className="chat-welcome">{agent.data && <AgentAvatar agent={agent.data} size="large" />}<h2>Talk to {agent.data?.name}</h2><p>{agent.data?.description || 'Send a message or attach an image to get started.'}</p></div> : messages.data?.messages.map(message => <MessageBubble key={message.id} message={message} agent={agent.data} inlineProcesses={messageTools.processes} hiddenToolCalls={messageTools.hiddenToolCalls} hiddenToolResults={messageTools.hiddenToolResults} />)}
		  {streamMessage && <><MessageBubble message={streamMessage} agent={agent.data} /><ToolTimeline tools={tools} /></>}
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
  </main>
}

function streamSeed(sessionID: number): Message { return { id: 'stream', session_id: sessionID, role: 'assistant', content: '', metadata: {}, created_at: new Date().toISOString() } }
