-- A user (one email) can take the diagnostic test only once.
-- If someone already has several results, keep their first one.
DELETE FROM diagnostic_results dr
USING diagnostic_results older
WHERE older.user_id = dr.user_id
  AND (older.created_at, older.id) < (dr.created_at, dr.id);

CREATE UNIQUE INDEX diagnostic_results_user_unique ON diagnostic_results (user_id);
