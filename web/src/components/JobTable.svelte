<script lang="ts">
  import type { Job } from '../lib/api'
  import { formatDuration, formatRelative } from '../lib/api'
  import StatusBadge from './StatusBadge.svelte'

  let { jobs, onSelect }: { jobs: Job[]; onSelect: (id: string) => void } = $props()
</script>

<div class="overflow-hidden rounded-lg border border-zinc-200 bg-white shadow-sm">
  <table class="min-w-full text-sm">
    <thead class="border-b border-zinc-200 bg-zinc-50 text-left text-xs uppercase tracking-wide text-zinc-500">
      <tr>
        <th class="px-4 py-3">Repo</th>
        <th class="px-4 py-3">Job</th>
        <th class="px-4 py-3">Status</th>
        <th class="px-4 py-3">Schedule</th>
        <th class="px-4 py-3">Duration</th>
        <th class="px-4 py-3">Updated</th>
      </tr>
    </thead>
    <tbody>
      {#each jobs as job}
        <tr
          class="cursor-pointer border-b border-zinc-100 hover:bg-zinc-50"
          onclick={() => onSelect(job.job_id)}
        >
          <td class="px-4 py-3 font-medium">{job.repo}</td>
          <td class="mono px-4 py-3 text-zinc-600">{job.job_id}</td>
          <td class="px-4 py-3"><StatusBadge status={job.status} /></td>
          <td class="px-4 py-3">{formatDuration(job.schedule_latency_sec)}</td>
          <td class="px-4 py-3">{formatDuration(job.job_duration_sec)}</td>
          <td class="px-4 py-3 text-zinc-500">{formatRelative(job.updated_at)}</td>
        </tr>
      {:else}
        <tr>
          <td colspan="6" class="px-4 py-8 text-center text-zinc-500">No jobs yet</td>
        </tr>
      {/each}
    </tbody>
  </table>
</div>
