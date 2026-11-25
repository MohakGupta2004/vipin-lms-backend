DROP INDEX IF EXISTS diagnostic_results_user_test_unique;

-- Back to one result per user: keep each user's first result.
DELETE FROM diagnostic_results dr
USING diagnostic_results older
WHERE older.user_id = dr.user_id
  AND (older.created_at, older.id) < (dr.created_at, dr.id);

CREATE UNIQUE INDEX diagnostic_results_user_unique ON diagnostic_results (user_id);

ALTER TABLE diagnostic_results DROP COLUMN IF EXISTS test_id;

DROP TABLE IF EXISTS diagnostic_questions;
DROP TABLE IF EXISTS diagnostic_tests;
