export type Job = {
  job_id: string
  run_id: string
  owner: string
  repo: string
  status: string
  webhook_at?: string
  dispatch_at?: string
  job_created_at?: string
  scheduled_at?: string
  running_at?: string
  completed_at?: string
  dispatch_latency_sec?: number
  schedule_latency_sec?: number
  job_duration_sec?: number
  cpu?: number
  arch?: string
  pool?: string
  cache_enabled?: boolean
  exit_code?: number
  pod_name?: string
  dispatch_error?: string
  github_url?: string
  updated_at?: string
}

export type TimelinePhase = {
  name: string
  at?: string
}

export type Stats = {
  dispatch_p50: number
  dispatch_p95: number
  schedule_p50: number
  schedule_p95: number
  dispatch_errors_24h: number
  active_jobs: number
  completed_jobs: number
}

export async function fetchStats(): Promise<Stats> {
  const res = await fetch('/api/v1/stats?since=24h')
  if (!res.ok) throw new Error('stats fetch failed')
  return res.json()
}

export async function fetchJobs(): Promise<Job[]> {
  const res = await fetch('/api/v1/jobs?limit=50')
  if (!res.ok) throw new Error('jobs fetch failed')
  const data = await res.json()
  return data.jobs ?? []
}

export async function fetchJob(id: string): Promise<{ job: Job; timeline: TimelinePhase[] }> {
  const res = await fetch(`/api/v1/jobs/${id}`)
  if (!res.ok) throw new Error('job fetch failed')
  return res.json()
}

export function formatDuration(sec?: number): string {
  if (sec == null || sec === 0) return '—'
  if (sec < 1) return `${(sec * 1000).toFixed(0)}ms`
  return `${sec.toFixed(1)}s`
}

export function formatRelative(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  const diff = Date.now() - d.getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}
