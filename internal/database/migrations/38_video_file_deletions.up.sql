-- Bucket folders of deleted videos that still have to be emptied. A row is queued in the same statement that
-- deletes the video and stays until every file (each rendition, the audio, the playlists) is gone, so a failed
-- or interrupted cleanup is retried instead of leaving files behind. One row per deletion: two batches' copies
-- of a video share a folder, and each copy's deletion re-checks whether the other still uses it.
CREATE TABLE video_file_deletions (
    id BIGSERIAL PRIMARY KEY,
    original_key TEXT NOT NULL,
    -- When the folder may next be emptied; a worker pushes it forward while it has the row claimed.
    run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX video_file_deletions_run_after_idx ON video_file_deletions (run_after);
