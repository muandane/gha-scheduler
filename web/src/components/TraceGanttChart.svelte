<script lang="ts">
  import { defineChart, rect } from '@tanstack/charts'
  import { Chart } from '@tanstack/charts/svelte'
  import type { ChartDefinition } from '@tanstack/charts/svelte'
  import { scaleBand, scaleUtc } from 'd3-scale'
  import type { Job, TimelinePhase } from '../lib/api'
  import {
    buildTraceRows,
    formatSpanDuration,
    formatTraceTime,
    rowOffsetPct,
    rowWidthPct,
    timeTicks,
    traceBounds,
    traceTimeDomain,
    type TraceRow,
  } from '../lib/traceChart'

  const ROW_HEIGHT = 34
  const LABEL_WIDTH = 300

  let {
    job,
    phases,
  }: {
    job: Job
    phases: TimelinePhase[]
  } = $props()

  let collapsed = $state(false)

  const rows = $derived(buildTraceRows(job, phases))
  const visibleRows = $derived(
    collapsed ? rows.filter((r) => r.depth === 0) : rows,
  )
  const bounds = $derived(traceBounds(rows))
  const ticks = $derived(timeTicks(bounds.totalMs))
  const root = $derived(rows.find((r) => r.id === 'root'))

  const minimapSpans = $derived(
    rows.filter((r) => r.id !== 'root').map((r) => ({
      ...r,
      lane: 'trace',
    })),
  )

  const minimapDefinition = $derived(
    defineChart({
      marks: [
        rect(minimapSpans, {
          x1: 'start',
          x2: 'end',
          y: 'lane',
          color: 'color',
          inset: 1,
          radius: 2,
        }),
      ],
      x: {
        scale: scaleUtc().domain(traceTimeDomain(rows)),
        grid: false,
        axis: false,
      },
      y: {
        scale: scaleBand<string>().domain(['trace']).paddingInner(0.2),
        grid: false,
        axis: false,
      },
      color: {
        domain: minimapSpans.map((s) => s.color),
        range: minimapSpans.map((s) => s.color),
      },
      margin: { top: 4, right: 8, bottom: 4, left: 8 },
    }) as ChartDefinition<(typeof minimapSpans)[number]>,
  )

  function rowTitle(row: TraceRow): string {
    return `${row.service}: ${row.operation}`
  }
</script>

{#if rows.length === 0}
  <p class="text-sm text-zinc-500">No trace timestamps yet — waiting for lifecycle events.</p>
{:else}
  <div class="overflow-hidden rounded-lg border border-zinc-800 bg-zinc-950 text-zinc-100 shadow-inner">
    <!-- Trace header -->
    <div class="border-b border-zinc-800 px-4 py-3">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <p class="truncate font-medium text-zinc-100">
            {root?.operation ?? 'workflow_job'}
            <span class="ml-2 font-normal text-teal-400">
              ({formatSpanDuration(bounds.totalMs)})
            </span>
          </p>
          <p class="mt-1 text-xs text-zinc-400">
            Start {formatTraceTime(bounds.start)} · job {job.job_id}
          </p>
        </div>
        <button
          type="button"
          class="rounded border border-zinc-700 px-2 py-1 text-xs text-zinc-300 hover:bg-zinc-900"
          onclick={() => (collapsed = !collapsed)}
        >
          {collapsed ? 'Expand spans' : 'Collapse spans'}
        </button>
      </div>
    </div>

    <!-- Minimap -->
    <div class="border-b border-zinc-800 bg-zinc-900/60 px-2 py-1">
      <Chart
        definition={minimapDefinition}
        height={44}
        ariaLabel="Trace overview"
        class="w-full opacity-90"
      />
    </div>

    <!-- Column headers -->
    <div
      class="grid border-b border-zinc-800 bg-zinc-900/80 text-xs uppercase tracking-wide text-zinc-500"
      style:grid-template-columns="{LABEL_WIDTH}px 1fr"
    >
      <div class="px-4 py-2">Service &amp; operation</div>
      <div class="relative px-3 py-2">
        <div class="relative h-4">
          {#each ticks as tick, i}
            <span
              class="absolute -translate-x-1/2 text-[10px]"
              style:left="{(i / Math.max(ticks.length - 1, 1)) * 100}%"
            >
              {formatSpanDuration(tick)}
            </span>
          {/each}
        </div>
      </div>
    </div>

    <!-- Tree + gantt rows -->
    <div class="max-h-[420px] overflow-y-auto">
      {#each visibleRows as row (row.id)}
        {@const left = rowOffsetPct(row.start, bounds.start, bounds.totalMs)}
        {@const width = rowWidthPct(row.start, row.end, bounds.totalMs)}
        {@const showLabel = width > 8}
        <div
          class="group grid border-b border-zinc-800/80 transition-colors hover:bg-zinc-900/50"
          style:grid-template-columns="{LABEL_WIDTH}px 1fr"
          style:height="{ROW_HEIGHT}px"
        >
          <!-- Operation tree -->
          <div
            class="flex items-center gap-1 overflow-hidden border-r border-zinc-800/80 px-3 text-xs"
            style:padding-left="{12 + row.depth * 16}px"
            title={rowTitle(row)}
          >
            {#if row.depth === 0}
              <span class="text-zinc-500">{collapsed ? '▸' : '▾'}</span>
            {/if}
            <span class="truncate text-zinc-400">{row.service}</span>
            <span class="truncate font-medium text-zinc-100">{row.operation}</span>
          </div>

          <!-- Timeline bar -->
          <div class="relative flex items-center px-3">
            <!-- Grid lines -->
            {#each ticks as _, i}
              <div
                class="pointer-events-none absolute inset-y-1 border-l border-zinc-800/70"
                style:left="{(i / Math.max(ticks.length - 1, 1)) * 100}%"
              ></div>
            {/each}

            {#if width > 0}
              <div
                class="absolute top-1/2 h-5 -translate-y-1/2 rounded-sm shadow-sm ring-1 ring-white/10"
                style:left="{left}%"
                style:width="{Math.max(width, 0.35)}%"
                style:background={row.color}
              >
                {#if showLabel}
                  <span
                    class="absolute left-full ml-1.5 whitespace-nowrap text-[11px] font-medium text-zinc-200"
                  >
                    {formatSpanDuration(row.durationMs)}
                  </span>
                {:else}
                  <span
                    class="absolute left-1/2 top-1/2 h-2 w-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-white/90"
                    title={formatSpanDuration(row.durationMs)}
                  ></span>
                {/if}
              </div>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  </div>
{/if}
