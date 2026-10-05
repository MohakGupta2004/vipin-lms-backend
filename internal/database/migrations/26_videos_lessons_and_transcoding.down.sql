DROP INDEX IF EXISTS videos_lesson_id_idx;

ALTER TABLE videos
    DROP COLUMN IF EXISTS transcode_job,
    DROP COLUMN IF EXISTS is_free,
    DROP COLUMN IF EXISTS lesson_id;
