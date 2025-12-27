-- CREATE TABLE users (
--     id SERIAL PRIMARY KEY,
--     username VARCHAR(50) NOT NULL UNIQUE,
--     email VARCHAR(100) NOT NULL UNIQUE,
--     password_hash VARCHAR(255) NOT NULL,
--     created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
--     updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
-- );

create type run_status as enum ('queued','running','success','failed','canceled');
create type job_status as enum ('pending','running','success','failed','canceled', 'queued');
create type step_status as enum ('pending','running','success','failed','canceled');

-- CREATE TABLE projects (
--     id SERIAL PRIMARY KEY,
--     name VARCHAR(100) NOT NULL,
--     repo_url VARCHAR(255) NOT NULL,
--     webhook_secret VARCHAR(255),
--     created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
--     updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
-- )            

-- One pipeline execution triggered by a push/PR/manual trigger
CREATE TABLE runs (
    id SERIAL PRIMARY KEY,
    status run_status NOT NULL DEFAULT 'queued',
    repo text NOT NULL,
    ref text NOT NULL,
    -- type VARCHAR(50) NOT NULL, -- 'ci' or 'preview'
    trigger VARCHAR(50) NOT NULL DEFAULT 'manual', -- 'manual', 'webhook'
    commit_sha TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    error_message TEXT
);

-- A job is a unit scheduled onto a runner (like Actions job)
CREATE TABLE jobs (
    id SERIAL PRIMARY KEY,
    run_id INTEGER REFERENCES runs(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    status job_status NOT NULL DEFAULT 'pending',
    needs text[] not null DEFAULT '{}',
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 1,
    runs_on text NOT NULL DEFAULT 'Linux',

    -- Leasing: the control plane assigns a runner and a lease timeout
    runner_id text,
    lease_expires_at TIMESTAMPTZ,
    leased_at        TIMESTAMPTZ,

    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_message TEXT,

    -- creates a composite unique constraint on the combination of the job_id and idx columns.
    UNIQUE (run_id, name)

);

-- Steps are executed in order on the runner
CREATE TABLE steps (
    id SERIAL PRIMARY KEY,
    job_id INTEGER REFERENCES jobs(id) ON DELETE CASCADE,
    idx INTEGER NOT NULL,
    name VARCHAR(100) NOT NULL,
    status step_status NOT NULL DEFAULT 'pending',
    command TEXT NOT NULL,
    exit_code INTEGER, 
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_message TEXT,
    UNIQUE (job_id, idx)
);

CREATE TABLE runners (
    id TEXT PRIMARY KEY,
    labels text[] NOT NULL DEFAULT '{}',
    last_heartbeat TIMESTAMP NOT NULL default now(),
    status text NOT NULL default 'online'
);

-- store logs in chunks (better than per-line)
CREATE TABLE log_chunks (
  id bigserial primary key,
  job_id bigint not null references jobs(id) on delete cascade,
  step_id bigint references steps(id) on delete cascade,
  timestamp timestamptz not null default now(),
  content text not null
);


CREATE INDEX log_chunks_job_id_id_idx on log_chunks(job_id, id);
CREATE INDEX jobs_run_id_idx on jobs(run_id);
CREATE INDEX idx_jobs_status ON jobs(status);

CREATE INDEX jobs_lease_expires_at_idx ON jobs(lease_expires_at) WHERE status = 'running';
CREATE INDEX idx_steps_status ON steps(status);

CREATE INDEX steps_job_id_idx on steps(job_id);