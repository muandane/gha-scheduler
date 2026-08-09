<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import {
    fetchJob,
    fetchJobs,
    fetchStats,
    type Job,
    type Stats,
    type TimelinePhase,
  } from './lib/api'
  import JobDetail from './components/JobDetail.svelte'
  import JobTable from './components/JobTable.svelte'
  import StatCard from './components/StatCard.svelte'

  let jobs: Job[] = $state([])
  let stats: Stats | null = $state(null)
  let selectedId: string | null = $state(null)
  let detailJob: Job | null = $state(null)
  let timeline: TimelinePhase[] = $state([])
  let error: string | null = $state(null)

  let timer: ReturnType<typeof setInterval> | undefined

  async function refresh() {
    try {
      ;[jobs, stats] = await Promise.all([fetchJobs(), fetchStats()])
      error = null
    } catch (e) {
      error = e instanceof Error ? e.message : 'refresh failed'
    }
  }

  async function openJob(id: string) {
    selectedId = id
    window.location.hash = `#/jobs/${id}`
    try {
      const data = await fetchJob(id)
      detailJob = data.job
      timeline = data.timeline ?? []
    } catch (e) {
      error = e instanceof Error ? e.message : 'load failed'
    }
  }

  function back() {
    selectedId = null
    detailJob = null
    window.location.hash = ''
  }

  function syncRoute() {
    const m = window.location.hash.match(/^#\/jobs\/(.+)$/)
    if (m) {
      openJob(m[1])
    } else {
      selectedId = null
      detailJob = null
    }
  }

  onMount(() => {
    refresh()
    timer = setInterval(refresh, 10000)
    syncRoute()
    window.addEventListener('hashchange', syncRoute)
  })

  onDestroy(() => {
    if (timer) clearInterval(timer)
    window.removeEventListener('hashchange', syncRoute)
  })
</script>

<div class="min-h-screen">
  <header class="border-b border-zinc-200 bg-white">
    <div class="mx-auto flex max-w-6xl items-center justify-between px-4 py-4">
      <div>
        <h1 class="text-lg font-semibold tracking-tight">gha-scheduler</h1>
        <p class="text-sm text-zinc-500">Job console</p>
      </div>
    </div>
  </header>

  <main class="mx-auto max-w-6xl space-y-6 px-4 py-8">
    {#if error}
      <div class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-800">{error}</div>
    {/if}

    {#if detailJob && selectedId}
      <JobDetail job={detailJob} {timeline} onBack={back} />
    {:else}
      <StatCard {stats} />
      <JobTable {jobs} onSelect={openJob} />
    {/if}
  </main>
</div>
