import { describe, expect, it } from 'vitest'
import type { Message, RunEvent } from './types'
import { buildConversationTurns, emptyLiveRun, reduceRunEvent } from './trace'

describe('buildConversationTurns', () => {
  it('preserves reasoning, tool result, and later reasoning order', () => {
    const messages: Message[] = [
      message(1, 'user', 'Investigate'),
      message(2, 'assistant', '', {
        reasoning_content: 'Plan.',
        tool_calls: [{ id: 'call_1', type: 'function', function: { name: 'lookup', arguments: '{"q":"x"}' } }],
        trace_steps: [
          { id: 'rs_1', kind: 'reasoning', status: 'completed', content: 'Plan.' },
          { id: 'call_1', kind: 'tool', tool_kind: 'function', name: 'lookup', status: 'preparing', arguments: '{"q":"x"}' },
        ],
      }),
      message(3, 'tool', 'model result', { tool_call_id: 'call_1', status: 'ok', display_content: 'visible result', structured_content: { value: 1 } }),
      message(4, 'assistant', 'Done.', {
        reasoning_content: 'Verify.',
        trace_steps: [
          { id: 'rs_2', kind: 'reasoning', status: 'completed', content: 'Verify.' },
          { id: 'ws_1', kind: 'tool', tool_kind: 'hosted', name: 'web_search', status: 'completed', detail: { action: { type: 'search', query: 'x' } } },
		  { id: 'rs_empty', kind: 'reasoning', status: 'completed' },
        ],
      }),
    ]

    const turns = buildConversationTurns(messages)
    expect(turns).toHaveLength(1)
    expect(turns[0].assistant?.content).toBe('Done.')
    expect(turns[0].trace.map(step => step.id)).toEqual(['rs_1', 'call_1', 'rs_2', 'ws_1'])
    expect(turns[0].trace[1]).toMatchObject({ status: 'completed', result: 'visible result', detail: { value: 1 } })
  })

  it('builds a compatible trace for historical metadata', () => {
    const turns = buildConversationTurns([
      message(1, 'user', 'Old turn'),
      message(2, 'assistant', '', { reasoning_content: 'Old reasoning', tool_calls: [{ id: 'old_call', type: 'function', function: { name: 'old_tool', arguments: '{}' } }] }),
      message(3, 'tool', 'old result', { tool_call_id: 'old_call', status: 'ok' }),
      message(4, 'assistant', 'Old answer'),
    ])
    expect(turns[0].trace.map(step => step.kind)).toEqual(['reasoning', 'tool'])
    expect(turns[0].trace[1].result).toBe('old result')
  })

  it('aggregates tool artifacts into the final assistant message', () => {
    const artifact = attachment('artifact-1', 'result.png', 'image/png')
    const turns = buildConversationTurns([
      message(1, 'user', 'Generate an image'),
      message(2, 'assistant', '', { tool_calls: [{ id: 'publish', type: 'function', function: { name: 'publish_attachment', arguments: '{}' } }] }),
      { ...message(3, 'tool', 'published', { tool_call_id: 'publish', status: 'ok', artifact_ids: [artifact.id] }), attachments: [artifact] },
      message(4, 'assistant', 'Done.'),
    ])
    expect(turns[0].assistant?.attachments).toEqual([artifact])
  })
})

describe('reduceRunEvent', () => {
  it('keeps live reasoning and tools interleaved and de-duplicates updates', () => {
    const events: RunEvent[] = [
      runEvent('reasoning.started', { id: 'rs_1', turn_index: 1, sequence: 1, status: 'running' }),
      runEvent('reasoning.delta', { id: 'rs_1', turn_index: 1, sequence: 2, delta: 'Plan.' }),
      runEvent('tool.started', { id: 'ws_1', turn_index: 1, sequence: 3, tool_kind: 'hosted', name: 'web_search', status: 'running' }),
      runEvent('tool.updated', { id: 'ws_1', turn_index: 1, sequence: 4, tool_kind: 'hosted', name: 'web_search', status: 'searching' }),
      runEvent('tool.completed', { id: 'ws_1', turn_index: 1, sequence: 5, tool_kind: 'hosted', name: 'web_search', status: 'completed', detail: { action: { query: 'x' } } }),
      runEvent('reasoning.started', { id: 'rs_2', turn_index: 1, sequence: 6, status: 'running' }),
      runEvent('reasoning.delta', { id: 'rs_2', turn_index: 1, sequence: 7, delta: 'Verify.' }),
      runEvent('assistant.delta', { turn_index: 1, delta: 'Final ' }),
      runEvent('assistant.delta', { turn_index: 1, delta: 'answer.' }),
    ]
    const state = events.reduce(reduceRunEvent, emptyLiveRun)
    expect(state.trace.map(step => step.id)).toEqual(['rs_1', 'ws_1', 'rs_2'])
    expect(state.trace[0].content).toBe('Plan.')
    expect(state.trace[1].status).toBe('completed')
    expect(state.trace[2].content).toBe('Verify.')
    expect(state.answer).toBe('Final answer.')
  })

  it('moves pre-tool assistant text into the trace instead of the final answer', () => {
    const state = [
      runEvent('assistant.delta', { turn_index: 1, delta: 'I will inspect.' }),
      runEvent('tool.started', { id: 'call_1', turn_index: 1, sequence: 1, name: 'lookup', status: 'running', arguments: {} }),
      runEvent('tool.completed', { id: 'call_1', turn_index: 1, sequence: 2, status: 'completed', content: 'ok' }),
      runEvent('reasoning.delta', { id: 'rs_2', turn_index: 2, sequence: 3, delta: 'Done.' }),
      runEvent('assistant.delta', { turn_index: 2, delta: 'Final.' }),
    ].reduce(reduceRunEvent, emptyLiveRun)
    expect(state.answer).toBe('Final.')
    expect(state.trace.map(step => step.kind)).toEqual(['commentary', 'tool', 'reasoning'])
  })

  it('adds live attachments once', () => {
    const artifact = attachment('artifact-1', 'report.zip', 'application/zip')
    const state = [
      runEvent('attachment.created', { attachment: artifact, turn_index: 1 }),
      runEvent('attachment.created', { attachment: artifact, turn_index: 1 }),
    ].reduce(reduceRunEvent, emptyLiveRun)
    expect(state.attachments).toEqual([artifact])
  })
})

function message(id: number, role: Message['role'], content: string, metadata?: Message['metadata']): Message {
  return { id, session_id: 1, role, content, metadata, created_at: new Date(0).toISOString() }
}

function runEvent(event: string, data: Record<string, unknown>): RunEvent { return { event, data } }

function attachment(id: string, name: string, mimeType: string) {
  return { id, session_id: 1, name, mime_type: mimeType, size_bytes: 12, created_at: new Date(0).toISOString() }
}
