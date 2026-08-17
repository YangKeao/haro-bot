import type { ReactNode } from 'react'
import { ChevronDown, Cpu, MemoryStick } from 'lucide-react'
import type { DisplayProcess } from '../processes'

export default function ProcessEntry({ process, className = '', children }: { process: DisplayProcess; className?: string; children?: ReactNode }) {
  const failed = process.status === 'failed' || (process.exit_code !== undefined && process.exit_code !== 0)
  return <details className={`process-item ${className}`.trim()}>
    <summary><span className={`process-state ${failed ? 'failed' : process.status}`} /> <code>{process.command}</code><span className="process-duration">{duration(process.duration_millis)}</span><ChevronDown /></summary>
    <div className="process-detail">
      <div className="process-meta">
        {process.pid !== undefined && <span><b>PID</b> {process.pid || '—'}</span>}
        {process.cpu_percent !== undefined && <span><Cpu /><b>CPU</b> {Number(process.cpu_percent).toFixed(1)}%</span>}
        {process.memory_bytes !== undefined && <span><MemoryStick /><b>RSS</b> {bytes(process.memory_bytes)}</span>}
        {process.exit_code !== undefined && <span><b>Exit</b> {process.exit_code}</span>}
		{Boolean(process.checks) && <span><b>Checks</b> {process.checks}{process.waiting ? ' · waiting' : ''}</span>}
      </div>
      <pre>{process.output || 'No output.'}{process.output_truncated ? '\n\n[Earlier output was truncated]' : ''}</pre>
      {children}
    </div>
  </details>
}

function duration(ms: number) { if (ms < 1000) return `${ms}ms`; const seconds = Math.floor(ms / 1000); if (seconds < 60) return `${seconds}s`; const minutes = Math.floor(seconds / 60); return `${minutes}m ${seconds % 60}s` }
function bytes(value: number) { if (!value) return '—'; if (value >= 1024 ** 3) return `${(value / 1024 ** 3).toFixed(1)} GiB`; if (value >= 1024 ** 2) return `${(value / 1024 ** 2).toFixed(1)} MiB`; return `${Math.ceil(value / 1024)} KiB` }
