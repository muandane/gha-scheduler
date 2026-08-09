<script lang="ts">
  import type { Stats } from '../lib/api'
  import { formatDuration } from '../lib/api'

  let { stats }: { stats: Stats | null } = $props()
</script>

<div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
  <div class="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
    <p class="text-xs uppercase tracking-wide text-zinc-500">Active</p>
    <p class="mt-1 text-2xl font-semibold">{stats?.active_jobs ?? '—'}</p>
  </div>
  <div class="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
    <p class="text-xs uppercase tracking-wide text-zinc-500">Schedule p95</p>
    <p class="mt-1 text-2xl font-semibold">{formatDuration(stats?.schedule_p95)}</p>
  </div>
  <div class="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
    <p class="text-xs uppercase tracking-wide text-zinc-500">Dispatch p95</p>
    <p class="mt-1 text-2xl font-semibold">{formatDuration(stats?.dispatch_p95)}</p>
  </div>
  <div class="rounded-lg border border-zinc-200 bg-white p-4 shadow-sm">
    <p class="text-xs uppercase tracking-wide text-zinc-500">Errors (24h)</p>
    <p class="mt-1 text-2xl font-semibold">{stats?.dispatch_errors_24h ?? '—'}</p>
  </div>
</div>
