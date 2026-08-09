# Garm runner/pool notes (cloudbase/garm v0.2.1)

Reference only — gha-scheduler does not import garm. Stateless k8s-Job dispatch
replaces garm's provider + state-store model (SPEC §6, ARCHITECTURE §2).

## JIT config

- **Fail closed on JIT errors** (garm issue #480): do not create a runner/pod if JIT
  generation fails after retries. garm historically created instances with nil JIT
  and fell back to registration tokens under rate limits — bad for security and ops.
  gha-scheduler: dispatch returns error after 3× retry; reconciler retries stale jobs.
- **GH runner orphan on partial failure**: garm `AddRunner` defers `RemoveEntityRunner`
  if instance create fails after JIT. gha-scheduler calls `DeleteRunner` on JIT
  success when Secret/Job creation fails (`internal/dispatch/dispatcher.go`).
- **Rate limits**: JIT + registration token paths share GH rate limits; App auth helps.

## Dedup / queued jobs

- **Lock before spawn** (pool `consumeQueuedJobs`): garm locks `workflow_job_id` in DB
  before `AddRunner` to avoid duplicate runners for the same job.
  gha-scheduler: in-process mutex per job ID + k8s Lease (`gha-dispatch-<job_id>`)
  before JIT; dedup via k8s Job label `gha-scheduler.gh_job_id`.
- **Job backoff**: garm waits `JobBackoff()` before spawning for a queued job so idle
  runners can pick up work first. We dispatch immediately on webhook — acceptable for
  per-job pods (no idle pool).
- **Stale queued jobs**: garm `reconcileStaleJobs` re-checks GH API for jobs queued
  >1h in local DB. Our reconciler polls GH for stale queued jobs without k8s Job —
  similar safety net.
- **Out-of-order webhooks**: garm uses 5m grace when deleting completed job records.
  Worth watching if `completed` arrives before `queued` during cutover.

## Runner naming

- garm: `{pool-prefix}-{random-id}` for global uniqueness.
- gha-scheduler: `ghs-{run_id}-{job_id}` — unique per workflow job; align with GH
  runner-name constraints in integration tests.

## Labels

- garm warns on jobs with no labels; passes label set to JIT API (same as us).
- Unknown label keys: we warn per SPEC §1; garm matches pools by full label set in DB.
