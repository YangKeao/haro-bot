import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, CircleStop, Cpu, Keyboard, LoaderCircle, MemoryStick, Skull, TerminalSquare } from 'lucide-react'
import { api, APIError } from '../api'

export default function ProcessPanel({ sessionID, enabled }: { sessionID: number; enabled: boolean }) {
  const client = useQueryClient()
  const [stdin, setStdin] = useState<Record<string, string>>({})
  const query = useQuery({ queryKey: ['session-processes', sessionID], queryFn: () => api.sessionProcesses(sessionID), enabled, retry: false, refetchInterval: enabled ? 2000 : false })
  const signal = useMutation({ mutationFn: ({ id, value }: { id: string; value: 'TERM' | 'KILL' }) => api.signalProcess(id, value), onSuccess: () => client.invalidateQueries({ queryKey: ['session-processes', sessionID] }) })
  const write = useMutation({ mutationFn: ({ id, chars }: { id: string; chars: string }) => api.processStdin(id, chars), onSuccess: (_, input) => { setStdin(values => ({ ...values, [input.id]: '' })); client.invalidateQueries({ queryKey: ['session-processes', sessionID] }) } })
  if (!enabled || (!query.isLoading && !query.data?.processes.length && !query.isError)) return null
  return <section className="process-panel" aria-label="Sandbox processes"><div className="process-panel-inner">
    <div className="process-panel-heading"><span><TerminalSquare /> Sandbox processes</span><small>{query.data?.processes.filter(item => item.status === 'running').length || 0} running · shared workspace</small></div>
    {query.isLoading ? <div className="process-loading"><LoaderCircle className="spin" /> Loading processes…</div> : query.isError ? <div className="process-error">{query.error instanceof APIError ? query.error.message : 'Could not load Sandbox processes.'}</div> : <div className="process-list">{query.data?.processes.map(process => <details className="process-item" key={process.id}>
      <summary><span className={`process-state ${process.status}`} /> <code>{process.command}</code><span className="process-duration">{duration(process.duration_millis)}</span><ChevronDown /></summary>
      <div className="process-detail"><div className="process-meta"><span><b>PID</b> {process.pid || '—'}</span><span><Cpu /><b>CPU</b> {Number(process.cpu_percent || 0).toFixed(1)}%</span><span><MemoryStick /><b>RSS</b> {bytes(process.memory_bytes || 0)}</span>{process.exit_code !== undefined && <span><b>Exit</b> {process.exit_code}</span>}</div>
        <pre>{process.output || 'No output yet.'}{process.output_truncated ? '\n\n[Earlier output was truncated]' : ''}</pre>
        {process.status === 'running' && <div className="process-controls"><form onSubmit={(event: FormEvent) => { event.preventDefault(); if (stdin[process.id]) write.mutate({ id: process.id, chars: stdin[process.id] }) }}><Keyboard /><input value={stdin[process.id] || ''} onChange={event => setStdin(values => ({ ...values, [process.id]: event.target.value }))} placeholder="Send stdin…" /><button disabled={!stdin[process.id] || write.isPending}>Send</button></form><button className="button secondary danger-text" disabled={signal.isPending} onClick={() => signal.mutate({ id: process.id, value: 'TERM' })}><CircleStop /> TERM</button><button className="button secondary danger-text" disabled={signal.isPending} onClick={() => signal.mutate({ id: process.id, value: 'KILL' })}><Skull /> KILL</button></div>}
      </div>
    </details>)}</div>}
  </div></section>
}

function duration(ms: number) { if (ms < 1000) return `${ms}ms`; const seconds = Math.floor(ms / 1000); if (seconds < 60) return `${seconds}s`; const minutes = Math.floor(seconds / 60); return `${minutes}m ${seconds % 60}s` }
function bytes(value: number) { if (!value) return '—'; if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GiB`; if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MiB`; return `${Math.ceil(value / 1024)} KiB` }
