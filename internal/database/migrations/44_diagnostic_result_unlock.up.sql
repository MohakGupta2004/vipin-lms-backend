-- A student sees their diagnostic result only once an admin unlocks it, usually after the strategy call.
-- NULL means locked; otherwise it is when the result was unlocked.
ALTER TABLE diagnostic_results ADD COLUMN unlocked_at timestamptz;
