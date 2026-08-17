import { describe, expect, it } from 'vitest'
import { aggregateProcessTools, runningProcesses } from './processes'
import type { SandboxProcess } from './types'

const process = (id: string, status: SandboxProcess['status']): SandboxProcess => ({
  id, status, sandbox_id: 1, agent_id: 1, session_id: 1, command: id,
  started_at: '2026-08-17T00:00:00Z', duration_millis: 10,
})

describe('runningProcesses', () => {
  it('keeps only processes that are currently running', () => {
    expect(runningProcesses([
      process('starting', 'starting'), process('running', 'running'), process('exited', 'exited'),
      process('failed', 'failed'), process('lost', 'lost'),
    ]).map(item => item.id)).toEqual(['running'])
  })
})

describe('aggregateProcessTools', () => {
  it('combines exec output and every write_stdin delta into one process', () => {
    const grouped = aggregateProcessTools([
      {
        id: 'exec-1', name: 'exec_command', arguments: { cmd: 'git clone repo', tty: false }, done: true,
        content: JSON.stringify({ chunk_id: 'a', wall_time_seconds: 10, output: 'Cloning\n', session_id: 'session-1' }),
      },
      {
        id: 'poll-1', name: 'write_stdin', arguments: { session_id: 'session-1', chars: '' }, done: true,
        content: JSON.stringify({ chunk_id: 'b', wall_time_seconds: 15, output: 'Receiving\n', session_id: 'session-1' }),
      },
      {
        id: 'poll-2', name: 'write_stdin', arguments: { session_id: 'session-1', chars: '' }, done: true,
        content: JSON.stringify({ chunk_id: 'c', wall_time_seconds: 18, output: 'Done\n', exit_code: 0 }),
      },
    ])

    expect(grouped.processes.get('exec-1')).toMatchObject({
      id: 'session-1', command: 'git clone repo', tty: false, status: 'exited', duration_millis: 18000,
      exit_code: 0, output: 'Cloning\nReceiving\nDone\n', checks: 2,
    })
    expect([...grouped.hiddenCallIDs]).toEqual(['exec-1', 'poll-1', 'poll-2'])
    expect([...grouped.hiddenResultIDs]).toEqual(['poll-1', 'poll-2'])
  })

  it('marks an active empty poll as waiting without showing another tool card', () => {
    const grouped = aggregateProcessTools([
      {
        id: 'exec-1', name: 'exec_command', arguments: '{"cmd":"server","tty":true}', done: true,
        content: '{"chunk_id":"a","wall_time_seconds":10,"output":"ready\\n","session_id":"session-1"}',
      },
      { id: 'poll-1', name: 'write_stdin', arguments: '{"session_id":"session-1","chars":""}' },
    ])
    expect(grouped.processes.get('exec-1')).toMatchObject({ status: 'running', checks: 1, waiting: true })
    expect(grouped.hiddenCallIDs.has('poll-1')).toBe(true)
  })

  it('keeps historical cumulative process output compatible', () => {
    const result = `Process ID: process-1
Status: exited
Wall time: 0.0420 seconds
Process exited with code 0
Output:
hello
`
    const grouped = aggregateProcessTools([{ id: 'old', name: 'exec_command', arguments: { cmd: 'printf hello' }, content: result, done: true }])
    expect(grouped.processes.get('old')).toMatchObject({
      id: 'process-1', command: 'printf hello', status: 'exited', duration_millis: 42,
      exit_code: 0, output: 'hello\n', output_truncated: false,
    })
  })

  it('leaves unrelated and malformed tool calls visible', () => {
    const grouped = aggregateProcessTools([
      { id: 'search', name: 'web_search', done: true, content: 'ok' },
      { id: 'bad', name: 'exec_command', arguments: { cmd: 'true' }, done: true, content: 'bad gateway' },
			{ id: 'exec', name: 'exec_command', arguments: { cmd: 'server' }, done: true, content: '{"chunk_id":"a","wall_time_seconds":10,"output":"","session_id":"session-1"}' },
			{ id: 'poll-error', name: 'write_stdin', arguments: { session_id: 'session-1' }, done: true, content: 'process is not running' },
    ])
		expect(grouped.processes.size).toBe(1)
		expect(grouped.hiddenCallIDs.has('bad')).toBe(false)
		expect(grouped.hiddenCallIDs.has('poll-error')).toBe(false)
  })
})
