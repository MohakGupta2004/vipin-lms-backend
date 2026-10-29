-- Deleting a course or lesson used to leave its videos' rows (hidden) and files behind. Delete those rows now
-- and queue their folders, as deleting a course or lesson does from here on. Batches' copies of a video keep
-- the files they still use: the worker checks before emptying a folder.
WITH gone AS (
    DELETE FROM videos v
    WHERE EXISTS (SELECT 1 FROM courses c WHERE c.id = v.course_id AND c.deleted_at IS NOT NULL)
       OR EXISTS (SELECT 1 FROM lessons l JOIN courses c ON c.id = l.course_id
                  WHERE l.id = v.lesson_id AND (l.deleted_at IS NOT NULL OR c.deleted_at IS NOT NULL))
    RETURNING original_key
)
INSERT INTO video_file_deletions (original_key) SELECT original_key FROM gone;
