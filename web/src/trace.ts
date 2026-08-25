import type { Message, RunEvent, TraceStep, ToolCall } from './types'

export type ConversationTurn = {
  id: string
  user?: Message
  assistant?: Message
  trace: TraceStep[]
  status?: string
}

export type LiveRunState = {
  answer: string
  answerTurn?: number
  trace: TraceStep[]
  toolTurns: number[]
}

export const emptyLiveRun: LiveRunState = { answer: '', trace: [], toolTurns: [] }

export function buildConversationTurns(messages: Message[] = []): ConversationTurn[] {
  const groups: Array<{ id: string; user?: Message; messages: Message[] }> = []
  for (const message of messages) {
    if (message.role === 'user') {
      groups.push({ id: String(message.id), user: message, messages: [] })
      continue
    }
    if (!groups.length) groups.push({ id: `orphan-${message.id}`, messages: [] })
    groups[groups.length - 1].messages.push(message)
  }

  return groups.map(group => {
    const trace: TraceStep[] = []
    let assistant: Message | undefined
    let lastAssistant: Message | undefined
    let status: string | undefined

    for (const message of group.messages) {
      const metadata = message.metadata || {}
      if (message.role === 'assistant') {
        lastAssistant = message
        status = metadata.status || status
        const steps = messageTraceSteps(message)
        appendTraceSteps(trace, steps)
        if (message.content && metadata.tool_calls?.length) {
          const firstTool = trace.findIndex(step => metadata.tool_calls?.some(call => call.id === step.id))
          const commentary: TraceStep = {
            id: `message-${message.id}-commentary`, kind: 'commentary', status: 'completed', content: message.content,
          }
          if (firstTool >= 0) trace.splice(firstTool, 0, commentary)
          else trace.push(commentary)
        }
        if (!metadata.tool_calls?.length) assistant = message
        continue
      }
      if (message.role === 'tool') mergeToolResult(trace, message)
    }

    if (!assistant && lastAssistant) assistant = { ...lastAssistant, content: '' }
    return { id: group.id, user: group.user, assistant, trace, status }
  })
}

function appendTraceSteps(trace: TraceStep[], steps: TraceStep[]) {
  for (const step of steps) {
    const index = trace.findIndex(existing => existing.id === step.id)
    if (index < 0) trace.push(step)
    else trace[index] = { ...trace[index], ...step }
  }
}

function messageTraceSteps(message: Message): TraceStep[] {
  const metadata = message.metadata || {}
  if (metadata.trace_steps?.length) {
    const steps = metadata.trace_steps
      .filter(step => step.kind !== 'reasoning' || Boolean(step.content))
      .map(step => ({ ...step }))
    appendMissingToolCalls(steps, metadata.tool_calls)
    return steps
  }
  const steps: TraceStep[] = []
  if (metadata.reasoning_content) {
    steps.push({
      id: `message-${message.id}-reasoning`, kind: 'reasoning', status: 'completed', content: metadata.reasoning_content,
    })
  }
  appendMissingToolCalls(steps, metadata.tool_calls)
  return steps
}

function appendMissingToolCalls(steps: TraceStep[], calls: ToolCall[] = []) {
  for (const call of calls) {
    if (steps.some(step => step.id === call.id)) continue
    steps.push({
      id: call.id, kind: 'tool', tool_kind: 'function', name: call.function.name,
      status: 'preparing', arguments: call.function.arguments,
    })
  }
}

function mergeToolResult(trace: TraceStep[], message: Message) {
  const id = message.metadata?.tool_call_id
  if (!id) return
  let step = trace.find(item => item.id === id)
  if (!step) {
    step = { id, kind: 'tool', tool_kind: 'function', name: message.metadata?.tool_name || 'tool' }
    trace.push(step)
  }
  step.status = message.metadata?.status === 'error' ? 'error' : 'completed'
  step.result = message.metadata?.display_content || message.content
  step.detail = message.metadata?.structured_content
}

export function reduceRunEvent(state: LiveRunState, event: RunEvent): LiveRunState {
  const data = event.data
  const turn = numberValue(data.turn_index) || 0
  if (event.event === 'assistant.delta') {
    const delta = stringValue(data.delta)
    if (!delta) return state
    if (state.toolTurns.includes(turn)) {
      return { ...state, trace: appendCommentary(state.trace, turn, delta) }
    }
    return {
      ...state,
      answer: state.answerTurn === undefined || state.answerTurn === turn ? state.answer + delta : delta,
      answerTurn: turn,
    }
  }
  if (event.event === 'assistant.completed') {
    const content = stringValue(data.content)
    return content && !state.answer ? { ...state, answer: content, answerTurn: turn } : state
  }
  if (event.event.startsWith('reasoning.')) {
    const id = stringValue(data.id) || `turn-${turn}-reasoning`
    const existing = state.trace.find(step => step.id === id)
    const delta = event.event === 'reasoning.delta' ? stringValue(data.delta) : ''
    const content = stringValue(data.content)
    const step: TraceStep = {
      id, kind: 'reasoning', status: event.event === 'reasoning.completed' ? 'completed' : 'running',
      content: content || (existing?.content || '') + delta,
      sequence: numberValue(data.sequence),
    }
    return { ...state, trace: upsertTrace(state.trace, step) }
  }
  if (event.event.startsWith('tool.')) {
    const id = stringValue(data.id)
    if (!id) return state
    const localTool = data.tool_kind !== 'hosted'
    let trace = state.trace
    let answer = state.answer
    let answerTurn = state.answerTurn
    if (localTool && !state.toolTurns.includes(turn) && answer && answerTurn === turn) {
      trace = appendCommentary(trace, turn, answer)
      answer = ''
      answerTurn = undefined
    }
    const existing = trace.find(step => step.id === id)
    const argumentsDelta = stringValue(data.arguments_delta)
    const result = stringValue(data.content) || stringValue(data.result)
    const step: TraceStep = {
      id,
      kind: 'tool',
      tool_kind: data.tool_kind === 'hosted' ? 'hosted' : existing?.tool_kind || 'function',
      name: stringValue(data.name) || existing?.name,
      status: traceStatus(data.status, event.event),
      arguments: argumentsDelta ? (existing?.arguments || '') + argumentsDelta : serializedValue(data.arguments) || existing?.arguments,
      result: result || existing?.result,
      detail: data.detail ?? existing?.detail,
      sequence: numberValue(data.sequence),
      truncated: Boolean(data.truncated || existing?.truncated),
    }
    return {
      ...state,
      answer,
      answerTurn,
      trace: upsertTrace(trace, step),
      toolTurns: !localTool || state.toolTurns.includes(turn) ? state.toolTurns : [...state.toolTurns, turn],
    }
  }
  if (event.event === 'run.failed' || event.event === 'run.cancelled') {
    const status = event.event === 'run.cancelled' ? 'cancelled' : 'error'
    return { ...state, trace: state.trace.map(step => step.status === 'running' || step.status === 'searching' || step.status === 'preparing' ? { ...step, status } : step) }
  }
  return state
}

function appendCommentary(trace: TraceStep[], turn: number, delta: string) {
  const id = `turn-${turn}-commentary`
  const existing = trace.find(step => step.id === id)
  return upsertTrace(trace, {
    id, kind: 'commentary', status: 'completed', content: (existing?.content || '') + delta,
  })
}

function upsertTrace(trace: TraceStep[], update: TraceStep) {
  const index = trace.findIndex(step => step.id === update.id)
  if (index < 0) return [...trace, update]
  const next = [...trace]
  next[index] = { ...next[index], ...update }
  return next
}

function traceStatus(value: unknown, event: string): TraceStep['status'] {
  if (value === 'preparing' || value === 'running' || value === 'searching' || value === 'completed' || value === 'error' || value === 'cancelled') return value
  return event === 'tool.completed' ? 'completed' : 'running'
}

function stringValue(value: unknown) { return typeof value === 'string' ? value : '' }
function numberValue(value: unknown) { return typeof value === 'number' ? value : undefined }
function serializedValue(value: unknown) {
  if (typeof value === 'string') return value
  if (value === undefined || value === null) return ''
  try { return JSON.stringify(value) } catch { return String(value) }
}
