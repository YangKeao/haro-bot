import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft, CircleStop, LoaderCircle, PlugZap, Radio, RotateCw, TerminalSquare } from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { api } from '../api'
import { useSandboxEvents } from '../sandboxEvents'

type ConnectionState = 'connecting' | 'connected' | 'disconnected' | 'unavailable'
type ServerMessage = { type: 'output' | 'exit' | 'error'; data?: string; message?: string; exit_code?: number }

export default function SandboxTerminal() {
  const { sandboxID } = useParams()
  const id = Number(sandboxID)
  const query = useQuery({ queryKey: ['sandbox', id], queryFn: () => api.sandbox(id), enabled: Number.isFinite(id), retry: false })
  const stream = useSandboxEvents(Boolean(query.data))
  const container = useRef<HTMLDivElement>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const [connection, setConnection] = useState<ConnectionState>('connecting')
  const [attempt, setAttempt] = useState(0)
  const profile = query.data?.sandbox
  const ready = profile?.runtime_status === 'Ready'
  const profileID = profile?.id
  const profileName = profile?.name || ''
  const runtimeStatus = profile?.runtime_status || 'Unavailable'

  useEffect(() => {
    if (!container.current || !profileID) return
    const terminal = new Terminal({
      cursorBlink: true,
      convertEol: true,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 13,
      theme: { background: '#1f211f', foreground: '#eeede8', cursor: '#86aaa3', selectionBackground: '#557d7866' },
    })
    const fit = new FitAddon()
    terminal.loadAddon(fit)
    terminal.open(container.current)
    fit.fit()
    if (!ready) {
      setConnection('unavailable')
      terminal.writeln(`\r\nSandbox is ${runtimeStatus}. The terminal will be available when it is Ready.`)
      return () => terminal.dispose()
    }

    setConnection('connecting')
    terminal.writeln(`Connecting to ${profileName}…\r\n`)
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/sandboxes/${id}/terminal`)
    socketRef.current = socket
    const sendResize = () => {
      if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'resize', columns: terminal.cols, rows: terminal.rows }))
    }
    socket.onopen = () => {
      setConnection('connected')
      terminal.clear()
      fit.fit()
      sendResize()
      terminal.focus()
    }
    socket.onmessage = event => {
      const message = JSON.parse(event.data) as ServerMessage
      if (message.type === 'output' && message.data) terminal.write(message.data)
      if (message.type === 'error') terminal.writeln(`\r\n\x1b[31m${message.message || 'Terminal error'}\x1b[0m`)
      if (message.type === 'exit') {
        terminal.writeln(`\r\n\x1b[90mProcess exited${message.exit_code == null ? '' : ` with code ${message.exit_code}`}.\x1b[0m`)
        setConnection('disconnected')
      }
    }
    socket.onerror = () => terminal.writeln('\r\n\x1b[31mTerminal connection failed.\x1b[0m')
    socket.onclose = () => {
      setConnection('disconnected')
      socketRef.current = null
    }
    const input = terminal.onData(data => {
      if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'input', data }))
    })
    const resize = terminal.onResize(sendResize)
    const observer = new ResizeObserver(() => { fit.fit(); sendResize() })
    observer.observe(container.current)
    return () => {
      observer.disconnect()
      input.dispose()
      resize.dispose()
      if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) socket.close(1000, 'page closed')
      socketRef.current = null
      terminal.dispose()
    }
  }, [attempt, id, profileID, profileName, ready, runtimeStatus])

  if (query.isLoading) return <div className="page-loading"><LoaderCircle className="spin" /></div>
  if (query.isError || !profile) return <main className="page terminal-page"><div className="empty-state card"><TerminalSquare /><h2>Terminal unavailable</h2><p>The Sandbox could not be loaded.</p><Link className="button secondary" to="/sandboxes">Back to sandboxes</Link></div></main>
  return <main className="page terminal-page">
    <header className="terminal-header"><div className="header-row"><Link to={`/sandboxes/${id}/edit`} className="icon-button" aria-label="Back"><ArrowLeft /></Link><div><div className="eyebrow">Sandbox terminal</div><h1>{profile.name}</h1></div></div><div className="terminal-header-status"><span className={`status-pill ${ready ? 'success' : 'warning'}`}>{profile.runtime_status}</span><span className={`live-indicator ${stream}`}><Radio /> {stream === 'live' ? 'Live' : 'Reconnecting'}</span><span className={`terminal-connection ${connection}`}><PlugZap /> {connection}</span>{connection === 'connected' ? <button className="button secondary" onClick={() => socketRef.current?.close(1000, 'disconnected by user')}><CircleStop /> Disconnect</button> : <button className="button primary" disabled={!ready || connection === 'connecting'} onClick={() => setAttempt(value => value + 1)}><RotateCw /> Reconnect</button>}</div></header>
    <section className="terminal-shell" aria-label={`${profile.name} terminal`}><div ref={container} className="terminal-surface" /></section>
    <footer className="terminal-footer"><span>Ephemeral shell · closes on disconnect</span><span>No Agent environment variables or secrets are injected</span></footer>
  </main>
}
