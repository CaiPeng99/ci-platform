-- -- DROP TABLE users;
-- DROP TABLE steps;
-- DROP TABLE jobs;
-- DROP TABLE runs;
-- -- DROP TABLE projects;    
-- DROP TYPE step_status;
-- DROP TYPE job_status;
-- DROP TYPE run_status;
-- DROP TABLE runners;
-- DROP TABLE log_chunks;

-- Drop indexes first (before dropping tables)
DROP INDEX IF EXISTS steps_job_id_idx;
DROP INDEX IF EXISTS idx_steps_status;
DROP INDEX IF EXISTS jobs_lease_expires_at_idx;
DROP INDEX IF EXISTS idx_jobs_status;
DROP INDEX IF EXISTS jobs_run_id_idx;
DROP INDEX IF EXISTS log_chunks_job_id_id_idx;

-- Drop tables in reverse order of creation (respecting foreign key dependencies)
DROP TABLE IF EXISTS artifacts CASCADE; 
DROP TABLE IF EXISTS log_chunks CASCADE;
DROP TABLE IF EXISTS steps CASCADE;
DROP TABLE IF EXISTS jobs CASCADE;           -- Then jobs
DROP TABLE IF EXISTS runs CASCADE;           -- Then runs
DROP TABLE IF EXISTS queue_status CASCADE;
DROP TABLE IF EXISTS runners CASCADE;

-- Drop types last (after tables that use them)
DROP TYPE IF EXISTS step_status CASCADE;
DROP TYPE IF EXISTS job_status CASCADE;
DROP TYPE IF EXISTS run_status CASCADE;