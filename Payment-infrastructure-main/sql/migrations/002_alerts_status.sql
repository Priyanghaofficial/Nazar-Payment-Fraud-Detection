-- Alert management (problem_statement.txt's "Alert management" capability) was schema-only
-- until now: the `alerts` table existed but no handler ever wrote or read it. This adds the
-- two columns a real open/resolved queue needs; everything else about the table is unchanged.
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'open';
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS resolved_at TIMESTAMPTZ;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS resolved_by TEXT;

CREATE INDEX IF NOT EXISTS alerts_open_idx ON alerts (raised_at DESC) WHERE status = 'open';
