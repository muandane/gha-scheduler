<script lang="ts">
  import type { Job } from '../lib/api'
  import { formatDuration } from '../lib/api'
  import StatusBadge from './StatusBadge.svelte'
  import TraceGanttChart from './TraceGanttChart.svelte'
  import type { TimelinePhase } from '../lib/api'

  let {
    job,
    timeline,
    onBack,
  }: {
    job: Job
    timeline: TimelinePhase[]
    onBack: () => void
  } = $props()
</script>

<div class="space-y-6">
  <button
    type="button"
    class="text-sm text-blue-600 hover:underline"
    onclick={onBack}
  >
    ← Back to jobs
  </button>

  <div class="flex flex-wrap items-start justify-between gap-4">
    <div>
      <h2 class="text-xl font-semibold">{job.repo}</h2>
      <p class="mono mt-1 text-sm text-zinc-500">job {job.job_id} · run {job.run_id}</p>
    </div>
    <StatusBadge status={job.status} />
  </div>

  {#if job.dispatch_error}
    <div class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">
      {job.dispatch_error}
    </div>
  {/if}

  <div class="flex flex-wrap gap-2 text-sm">
    {#if job.cpu}<span class="rounded-full bg-zinc-100 px-3 py-1">cpu={job.cpu}</span>{/if}
    {#if job.arch}<span class="rounded-full bg-zinc-100 px-3 py-1">{job.arch}</span>{/if}
    {#if job.pool}<span class="rounded-full bg-zinc-100 px-3 py-1">pool={job.pool}</span>{/if}
    {#if job.cache_enabled}<span class="rounded-full bg-zinc-100 px-3 py-1">cache</span>{/if}
  </div>

  <div class="space-y-6">
    <div class="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
      <h3 class="mb-4 font-medium">Trace detail</h3>
      <TraceGanttChart {job} phases={timeline} />
    </div>

    <div class="grid gap-6 lg:grid-cols-2">
      <div class="rounded-lg border border-zinc-200 bg-white p-6 shadow-sm">
        <h3 class="mb-4 font-medium">Latencies</h3>
      <dl class="space-y-3 text-sm">
        <div class="flex justify-between"><dt class="text-zinc-500">Dispatch</dt><dd>{formatDuration(job.dispatch_latency_sec)}</dd></div>
        <div class="flex justify-between"><dt class="text-zinc-500">Schedule</dt><dd>{formatDuration(job.schedule_latency_sec)}</dd></div>
        <div class="flex justify-between"><dt class="text-zinc-500">Run</dt><dd>{formatDuration(job.job_duration_sec)}</dd></div>
      </dl>
      {#if job.github_url}
        <a
          href={job.github_url}
          target="_blank"
          rel="noopener noreferrer"
          class="mt-6 inline-flex text-sm font-medium text-blue-600 hover:underline"
        >
          View on GitHub →
        </a>
      {/if}
      </div>
    </div>
  </div>
</div>
