<script lang="ts">
  let { status }: { status: string } = $props()

  const colors: Record<string, string> = {
    queued: 'bg-zinc-100 text-zinc-700',
    dispatching: 'bg-amber-100 text-amber-800',
    dispatched: 'bg-amber-100 text-amber-800',
    scheduled: 'bg-blue-100 text-blue-800',
    running: 'bg-blue-100 text-blue-800',
    succeeded: 'bg-emerald-100 text-emerald-800',
    failed: 'bg-red-100 text-red-800',
    dispatch_error: 'bg-red-100 text-red-800',
  }
</script>

<span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium {colors[status] ?? 'bg-zinc-100 text-zinc-700'}">
  {status}
</span>
