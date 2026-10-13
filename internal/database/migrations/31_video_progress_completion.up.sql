-- Completion lets students (and their progress page) tell a finished video from one they only opened.
-- duration_sec is what the player reported for this user, since videos.duration_sec is not filled in yet.
ALTER TABLE video_progress
    ADD COLUMN duration_sec INT CHECK (duration_sec >= 0),
    ADD COLUMN completed_at TIMESTAMPTZ;
