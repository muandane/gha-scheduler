<script lang="ts">
  import { defineChart, rect } from '@tanstack/charts'
  import { tooltip } from '@tanstack/charts/tooltip'
  import { Chart } from '@tanstack/charts/svelte'
  import type { ChartDefinition } from '@tanstack/charts/svelte'
  import { scaleBand, scaleUtc } from 'd3-scale'
  import type { Job } from '../lib/api'
  import { formatDuration } from '../lib/api'
  import {
    buildJobsOverviewBars,
    formatTraceTime,
    statusColor,
    traceTimeDomain,
    type JobTraceBar,
  } from '../lib/traceChart'

  let {
    jobs,
  }: {
    jobs: Job[]
  } = $props()

  const bars = $derived(buildJobsOverviewBars(jobs))
  const labels = $derived(bars.map((b) => b.label))
  const domain = $derived(traceTimeDomain(bars))
  const statuses = $derived([...new Set(bars.map((b) => b.status))])
  const chartHeight = $derived(Math.max(180, bars.length * 36 + 48))

  const definition = $derived(
    defineChart(
      {
        marks: [
          rect(bars, {
            x1: 'start',
            x2: 'end',
            y: 'label',
            color: 'status',
            inset: 4,
            radius: 3,
            stroke: '#ffffff',
            strokeWidth: 1,
          }),
        ],
        x: {
          scale: scaleUtc().domain(domain),
          grid: true,
          axis: { ticks: { count: 5 } },
        },
        y: {
          scale: scaleBand<string>()
            .domain(labels)
            .paddingInner(0.18)
            .paddingOuter(0.08),
          grid: false,
          axis: {},
        },
        color: {
          domain: statuses,
          range: statuses.map((status) => statusColor(status)),
        },
        margin: { top: 8, right: 12, bottom: 32, left: 128 },
      },
      {
        tooltip: {
          use: tooltip,
          format: (point) => {
            const bar = point.datum as JobTraceBar
            return `${bar.repo} · ${bar.status} · ${formatDuration(bar.durationSec)} · ${formatTraceTime(bar.start)}`
          },
        },
        keyboard: true,
      },
    ) as ChartDefinition<JobTraceBar>,
  )
</script>

{#if bars.length === 0}
  <p class="text-sm text-zinc-500">No job traces in the current window.</p>
{:else}
  <Chart
    definition={definition}
    height={chartHeight}
    ariaLabel="Recent job traces overview"
    ariaDescription="Gantt overview of recent workflow jobs"
    class="w-full"
  />
{/if}
