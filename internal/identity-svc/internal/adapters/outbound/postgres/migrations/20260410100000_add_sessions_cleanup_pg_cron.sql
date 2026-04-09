-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_cron;

CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at);

CREATE INDEX IF NOT EXISTS idx_sessions_revoked_at ON sessions (revoked_at)
WHERE
  revoked_at IS NOT NULL;

DO $cleanup_expired$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM cron.job
    WHERE jobname = 'cleanup_expired_sessions'
  ) THEN
    PERFORM (
      SELECT cron.schedule(
        'cleanup_expired_sessions',
        '0 */3 * * *',
        $$DELETE FROM sessions WHERE expires_at < NOW()$$
      )
    );
  END IF;
END;
$cleanup_expired$;

DO $cleanup_revoked$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM cron.job
    WHERE jobname = 'cleanup_revoked_sessions'
  ) THEN
    PERFORM (
      SELECT cron.schedule(
        'cleanup_revoked_sessions',
        '0 2 * * *',
        $$DELETE FROM sessions WHERE revoked_at IS NOT NULL AND revoked_at < NOW() - INTERVAL '7 days'$$
      )
    );
  END IF;
END;
$cleanup_revoked$;

-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $unschedule_jobs$
DECLARE
  scheduled_job RECORD;
BEGIN
  FOR scheduled_job IN
    SELECT jobid
    FROM cron.job
    WHERE jobname IN ('cleanup_expired_sessions', 'cleanup_revoked_sessions')
  LOOP
    PERFORM cron.unschedule(scheduled_job.jobid);
  END LOOP;
END;
$unschedule_jobs$;

DROP EXTENSION IF EXISTS pg_cron CASCADE;

DROP INDEX IF EXISTS idx_sessions_revoked_at;

DROP INDEX IF EXISTS idx_sessions_expires_at;

-- +goose StatementEnd
