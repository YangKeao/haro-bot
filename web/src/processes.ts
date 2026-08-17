import type { SandboxProcess } from './types'

export type DisplayProcess = Pick<SandboxProcess, 'id' | 'command' | 'status' | 'duration_millis'> &
  Partial<Pick<SandboxProcess, 'tty' | 'pid' | 'exit_code' | 'cpu_percent' | 'memory_bytes' | 'output' | 'output_truncated'>> & {
    checks?: number
    waiting?: boolean
  }

export type ProcessToolActivity = {
  id: string
  name: string
  arguments?: unknown
  content?: string
  done?: boolean
}

type ParsedResult = {
  id?: string
  status: SandboxProcess['status']
  duration: number
  exitCode?: number
  output: string
  truncated: boolean
  incremental: boolean
}

export function runningProcesses(processes?: SandboxProcess[]) {
  return processes?.filter(process => process.status === 'running') || []
}

export function aggregateProcessTools(tools: ProcessToolActivity[]) {
  const processes = new Map<string, DisplayProcess>()
  const sessions = new Map<string, string>()
  const hiddenCallIDs = new Set<string>()
  const hiddenResultIDs = new Set<string>()

  for (const tool of tools) {
    if (tool.name === 'exec_command' && tool.done && tool.content) {
      const args = commandArguments(tool.arguments)
      const result = parseResult(tool.content)
      if (!args.cmd || !result) continue
      const process: DisplayProcess = {
        id: result.id || `tool-${tool.id}`,
        command: args.cmd,
        tty: args.tty,
        status: result.status,
        duration_millis: result.duration,
        exit_code: result.exitCode,
        output: result.output,
        output_truncated: result.truncated,
        checks: 0,
      }
      processes.set(tool.id, process)
      if (result.id) sessions.set(result.id, tool.id)
      hiddenCallIDs.add(tool.id)
      continue
    }

    if (tool.name !== 'write_stdin') continue
    const sessionID = sessionArgument(tool.arguments)
    const primaryID = sessionID ? sessions.get(sessionID) : undefined
    const process = primaryID ? processes.get(primaryID) : undefined
    if (!primaryID || !process) continue
    if (!tool.done) {
      hiddenCallIDs.add(tool.id)
      hiddenResultIDs.add(tool.id)
      process.checks = (process.checks || 0) + 1
      process.waiting = true
      continue
    }
    const result = tool.content ? parseResult(tool.content) : undefined
    if (!result) continue
    hiddenCallIDs.add(tool.id)
    hiddenResultIDs.add(tool.id)
    process.checks = (process.checks || 0) + 1
    process.status = result.status
    process.duration_millis = Math.max(process.duration_millis, result.duration)
    process.exit_code = result.exitCode
    process.output = result.incremental ? `${process.output || ''}${result.output}` : result.output
    process.output_truncated = Boolean(process.output_truncated || result.truncated)
    process.waiting = false
  }

  return { processes, hiddenCallIDs, hiddenResultIDs }
}

function parseResult(content: string): ParsedResult | undefined {
  const structured = structuredResult(content)
  if (structured) return structured
  return legacyResult(content)
}

function structuredResult(content: string): ParsedResult | undefined {
  let value: unknown
  try { value = JSON.parse(content) } catch { return undefined }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  const result = value as Record<string, unknown>
  if (typeof result.chunk_id !== 'string' || typeof result.output !== 'string' || typeof result.wall_time_seconds !== 'number') return undefined
  const running = typeof result.session_id === 'string' && result.session_id.length > 0
  const exitCode = typeof result.exit_code === 'number' ? result.exit_code : undefined
  return {
    id: running ? String(result.session_id) : undefined,
    status: running ? 'running' : exitCode === 0 ? 'exited' : 'failed',
    duration: Math.round(Number(result.wall_time_seconds) * 1000),
    exitCode,
    output: String(result.output),
    truncated: typeof result.original_token_count === 'number',
    incremental: true,
  }
}

function legacyResult(content: string): ParsedResult | undefined {
  const id = field(content, 'Process ID')
  const status = field(content, 'Status') as SandboxProcess['status'] | undefined
  if (!id || !status || !['starting', 'running', 'exited', 'failed'].includes(status)) return undefined
  const wallTime = Number.parseFloat(field(content, 'Wall time')?.replace(/\s+seconds?$/, '') || '')
  const exitMatch = content.match(/^Process exited with code (-?\d+)$/m)
  const outputMarker = content.match(/\nOutput:\r?\n/)
  return {
    id,
    status,
    duration: Number.isFinite(wallTime) ? Math.round(wallTime * 1000) : 0,
    exitCode: exitMatch ? Number(exitMatch[1]) : undefined,
    output: outputMarker?.index === undefined ? '' : content.slice(outputMarker.index + outputMarker[0].length),
    truncated: content.includes('Output is truncated to the most recent data.'),
    incremental: false,
  }
}

function commandArguments(value: unknown): { cmd?: string; tty?: boolean } {
  const args = objectArguments(value)
  return {
    cmd: typeof args?.cmd === 'string' ? args.cmd : undefined,
    tty: args?.tty === true,
  }
}

function sessionArgument(value: unknown) {
  const args = objectArguments(value)
  if (typeof args?.session_id === 'string') return args.session_id
  // Historical messages used process_id before the Codex-compatible protocol.
  return typeof args?.process_id === 'string' ? args.process_id : undefined
}

function objectArguments(value: unknown): Record<string, unknown> | undefined {
  if (typeof value === 'string') {
    try { return objectArguments(JSON.parse(value)) } catch { return undefined }
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  return value as Record<string, unknown>
}

function field(content: string, name: string) {
  return content.match(new RegExp(`^${name}:\\s*(.+)$`, 'm'))?.[1]?.trim()
}
