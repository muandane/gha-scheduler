import type { Job, TimelinePhase } from './api'

export type TraceSpan = {
  lane: string
  phase: string
  start: Date
  end: Date
  durationSec: number
}

export type JobTraceBar = {
  jobId: string
  label: string
  repo: string
  status: string
  start: Date
  end: Date
  durationSec: number
}

/** One row in the Perses-style trace tree + gantt grid. */
export type TraceRow = {
  id: string
  parentId?: string
  depth: number
  service: string
  operation: string
  start: Date
  end: Date
  durationMs: number
  color: string
}

const PHASE_ORDER = [
  'webhook_received',
  'dispatch',
  'job_created',
  'pod_scheduled',
  'pod_running',
  'pod_completed',
] as const

const PHASE_LABELS: Record<string, string> = {
  webhook_received: 'Webhook received',
  dispatch: 'Dispatch',
  job_created: 'Job created',
  pod_scheduled: 'Pod scheduled',
  pod_running: 'Pod running',
  pod_completed: 'Pod completed',
}

const SERVICE_FOR_PHASE: Record<string, string> = {
  webhook_received: 'gha-scheduler',
  dispatch: 'gha-scheduler',
  job_created: 'gha-scheduler',
  pod_scheduled: 'kubernetes',
  pod_running: 'kubernetes',
  pod_completed: 'actions-runner',
}

const PHASE_COLORS: Record<string, string> = {
  webhook_received: '#14b8a6',
  dispatch: '#3b82f6',
  job_created: '#6366f1',
  pod_scheduled: '#8b5cf6',
  pod_running: '#06b6d4',
  pod_completed: '#22c55e',
}

const STATUS_COLORS: Record<string, string> = {
  succeeded: '#22c55e',
  running: '#3b82f6',
  failed: '#ef4444',
  dispatch_error: '#ef4444',
  queued: '#94a3b8',
  dispatching: '#f59e0b',
  dispatched: '#06b6d4',
  scheduled: '#8b5cf6',
}

export function phaseLabel(name: string): string {
  return PHASE_LABELS[name] ?? name
}

export function laneColor(phase: string, lane?: string): string {
  if (phase === 'total') return '#6366f1'
  const key = Object.entries(PHASE_LABELS).find(([, label]) => label === lane)?.[0]
  if (key && PHASE_COLORS[key]) return PHASE_COLORS[key]
  return '#64748b'
}

export function statusColor(status: string): string {
  return STATUS_COLORS[status] ?? '#64748b'
}

function parseAt(iso?: string): Date | undefined {
  if (!iso) return undefined
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? undefined : d
}

function phaseMap(phases: TimelinePhase[]): Map<string, Date> {
  const map = new Map<string, Date>()
  for (const phase of phases) {
    const at = parseAt(phase.at)
    if (at) map.set(phase.name, at)
  }
  return map
}

function msBetween(start: Date, end: Date): number {
  return Math.max(0, end.getTime() - start.getTime())
}

/** Format duration like Perses/Tempo (µs / ms / s). */
export function formatSpanDuration(ms: number): string {
  if (ms < 1) return `${Math.round(ms * 1000)}µs`
  if (ms < 1000) return `${ms < 10 ? ms.toFixed(2) : ms < 100 ? ms.toFixed(1) : Math.round(ms)}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)}s`
  const mins = Math.floor(ms / 60_000)
  const secs = ((ms % 60_000) / 1000).toFixed(0)
  return `${mins}m ${secs}s`
}

export function formatTraceTime(d: Date): string {
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    fractionalSecondDigits: 3,
  })
}

export function traceBounds(
  rows: ReadonlyArray<{ start: Date; end: Date }>,
): { start: Date; end: Date; totalMs: number } {
  if (rows.length === 0) {
    const now = new Date()
    return { start: now, end: now, totalMs: 0 }
  }
  let min = rows[0].start.getTime()
  let max = rows[0].end.getTime()
  for (const row of rows) {
    min = Math.min(min, row.start.getTime())
    max = Math.max(max, row.end.getTime())
  }
  const start = new Date(min)
  const end = new Date(max)
  return { start, end, totalMs: Math.max(1, max - min) }
}

export function traceTimeDomain(
  spans: ReadonlyArray<{ start: Date; end: Date }>,
): [Date, Date] {
  const { start, end } = traceBounds(spans)
  const pad = Math.max((end.getTime() - start.getTime()) * 0.04, 250)
  return [new Date(start.getTime() - pad), new Date(end.getTime() + pad)]
}

export function rowOffsetPct(
  rowStart: Date,
  traceStart: Date,
  totalMs: number,
): number {
  if (totalMs <= 0) return 0
  return ((rowStart.getTime() - traceStart.getTime()) / totalMs) * 100
}

export function rowWidthPct(
  rowStart: Date,
  rowEnd: Date,
  totalMs: number,
): number {
  if (totalMs <= 0) return 0
  return (msBetween(rowStart, rowEnd) / totalMs) * 100
}

export function timeTicks(totalMs: number, count = 5): number[] {
  if (totalMs <= 0) return [0]
  const step = totalMs / (count - 1)
  return Array.from({ length: count }, (_, i) => Math.round(step * i))
}

/** Perses-style hierarchical rows for one job trace. */
export function buildTraceRows(job: Job, phases: TimelinePhase[]): TraceRow[] {
  const at = phaseMap(phases)
  const traceEnd =
    at.get('pod_completed') ??
    parseAt(job.completed_at) ??
    parseAt(job.updated_at) ??
    new Date()
  const traceStart = at.get('webhook_received') ?? parseAt(job.webhook_at)
  if (!traceStart) return []

  const repo = job.repo.includes('/') ? job.repo : `${job.owner}/${job.repo}`
  const root: TraceRow = {
    id: 'root',
    depth: 0,
    service: 'gha-scheduler',
    operation: `${repo}: workflow_job`,
    start: traceStart,
    end: traceEnd,
    durationMs: msBetween(traceStart, traceEnd),
    color: '#14b8a6',
  }

  const children: TraceRow[] = []
  for (let i = 0; i < PHASE_ORDER.length - 1; i++) {
    const from = PHASE_ORDER[i]
    const to = PHASE_ORDER[i + 1]
    const start = at.get(from)
    const end = at.get(to)
    if (!start || !end || end <= start) continue
    children.push({
      id: to,
      parentId: 'root',
      depth: 1,
      service: SERVICE_FOR_PHASE[to] ?? 'gha-scheduler',
      operation: `job.${to}`,
      start,
      end,
      durationMs: msBetween(start, end),
      color: PHASE_COLORS[to] ?? '#64748b',
    })
  }

  return [root, ...children]
}

/** Flat span list for minimap / legacy charts. */
export function buildJobTraceSpans(
  job: Job,
  phases: TimelinePhase[],
): TraceSpan[] {
  return buildTraceRows(job, phases).map((row) => ({
    lane: row.operation,
    phase: row.id,
    start: row.start,
    end: row.end,
    durationSec: row.durationMs / 1000,
  }))
}

export function uniqueLanes(spans: TraceSpan[]): string[] {
  return spans.map((s) => s.lane)
}

export function buildJobsOverviewBars(jobs: Job[]): JobTraceBar[] {
  return jobs
    .map((job) => {
      const start = parseAt(job.webhook_at)
      if (!start) return null
      const end =
        parseAt(job.completed_at) ??
        parseAt(job.running_at) ??
        parseAt(job.updated_at) ??
        new Date()
      if (end <= start) return null
      const repo = job.repo.includes('/') ? job.repo.split('/').pop()! : job.repo
      return {
        jobId: job.job_id,
        label: `${repo} · ${job.job_id.slice(-6)}`,
        repo: job.repo,
        status: job.status,
        start,
        end,
        durationSec: (end.getTime() - start.getTime()) / 1000,
      }
    })
    .filter((row): row is JobTraceBar => row !== null)
    .slice(0, 12)
}
