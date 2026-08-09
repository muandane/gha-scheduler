<script lang="ts">
  import type { TimelinePhase } from '../lib/api'
  import { formatRelative } from '../lib/api'

  let { phases }: { phases: TimelinePhase[] } = $props()

  const labels: Record<string, string> = {
    webhook_received: 'Webhook',
    dispatch: 'Dispatch',
    job_created: 'Job created',
    pod_scheduled: 'Scheduled',
    pod_running: 'Running',
    pod_completed: 'Completed',
  }
</script>

<ol class="space-y-3">
  {#each phases as phase, i}
    <li class="flex items-start gap-3">
      <div class="flex flex-col items-center">
        <div class="flex h-8 w-8 items-center justify-center rounded-full bg-blue-600 text-xs font-semibold text-white">
          {i + 1}
        </div>
        {#if i < phases.length - 1}
          <div class="mt-1 h-8 w-px bg-zinc-200"></div>
        {/if}
      </div>
      <div class="pt-1">
        <p class="font-medium">{labels[phase.name] ?? phase.name}</p>
        <p class="text-sm text-zinc-500">{formatRelative(phase.at)}</p>
      </div>
    </li>
  {/each}
</ol>
