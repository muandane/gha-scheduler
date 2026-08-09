CREATE TABLE jobs (
    job_id                 TEXT PRIMARY KEY,
    run_id                 TEXT NOT NULL,
    owner                  TEXT NOT NULL,
    repo                   TEXT NOT NULL,
    status                 TEXT NOT NULL DEFAULT 'queued',
    webhook_at             TEXT,
    dispatch_at            TEXT,
    job_created_at         TEXT,
    scheduled_at           TEXT,
    running_at             TEXT,
    completed_at           TEXT,
    dispatch_latency_sec   REAL,
    schedule_latency_sec   REAL,
    job_duration_sec       REAL,
    cpu                    INTEGER,
    arch                   TEXT,
    pool                   TEXT,
    cache_enabled          INTEGER NOT NULL DEFAULT 0,
    labels_json            TEXT,
    exit_code              INTEGER,
    pod_name               TEXT,
    dispatch_error         TEXT,
    created_at             TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at             TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_jobs_repo_updated ON jobs(repo, updated_at DESC);
CREATE INDEX idx_jobs_run_id ON jobs(run_id);
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_completed_at ON jobs(completed_at);
