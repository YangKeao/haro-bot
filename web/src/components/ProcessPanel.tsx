import { useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleStop, Keyboard, LoaderCircle, Skull, TerminalSquare } from 'lucide-react'
import { api, APIError } from '../api'
import { runningProcesses } from '../processes'
import ProcessEntry from './ProcessEntry'

export default function ProcessPanel({ sessionID, enabled }: { sessionID: number; enabled: boolean }) {
  const client = useQueryClient()
  const [stdin, setStdin] = useState<Record<string, string>>({})
  const query = useQuery({ queryKey: ['session-processes', sessionID], queryFn: () => api.sessionProcesses(sessionID), enabled, retry: false, refetchInterval: enabled ? 2000 : false })
  const signal = useMutation({ mutationFn: ({ id, value }: { id: string; value: 'TERM' | 'KILL' }) => api.signalProcess(id, value), onSuccess: () => client.invalidateQueries({ queryKey: ['session-processes', sessionID] }) })
  const write = useMutation({ mutationFn: ({ id, chars }: { id: string; chars: string }) => api.processStdin(id, chars), onSuccess: (_, input) => { setStdin(values => ({ ...values, [input.id]: '' })); client.invalidateQueries({ queryKey: ['session-processes', sessionID] }) } })
  const processes = runningProcesses(query.data?.processes)
  if (!enabled || (!query.isLoading && !processes.length && !query.isError)) return null
  return <section className="process-panel" aria-label="Sandbox processes"><div className="process-panel-inner">
    <div className="process-panel-heading"><span><TerminalSquare /> Sandbox processes</span><small>{processes.length} running · shared workspace</small></div>
    {query.isLoading ? <div className="process-loading"><LoaderCircle className="spin" /> Loading processes…</div> : query.isError ? <div className="process-error">{query.error instanceof APIError ? query.error.message : 'Could not load Sandbox processes.'}</div> : <div className="process-list">{processes.map(process => <ProcessEntry process={process} key={process.id}>
      <div className="process-controls">{process.tty !== false && <form onSubmit={(event: FormEvent) => { event.preventDefault(); if (stdin[process.id]) write.mutate({ id: process.id, chars: stdin[process.id] }) }}><Keyboard /><input value={stdin[process.id] || ''} onChange={event => setStdin(values => ({ ...values, [process.id]: event.target.value }))} placeholder="Send stdin…" /><button disabled={!stdin[process.id] || write.isPending}>Send</button></form>}<button className="button secondary danger-text" disabled={signal.isPending} onClick={() => signal.mutate({ id: process.id, value: 'TERM' })}><CircleStop /> TERM</button><button className="button secondary danger-text" disabled={signal.isPending} onClick={() => signal.mutate({ id: process.id, value: 'KILL' })}><Skull /> KILL</button></div>
    </ProcessEntry>)}</div>}
  </div></section>
}
