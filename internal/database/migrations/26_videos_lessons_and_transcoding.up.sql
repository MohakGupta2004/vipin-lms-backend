ALTER TABLE videos
    ADD COLUMN lesson_id UUID REFERENCES lessons(id) ON DELETE CASCADE,
    ADD COLUMN is_free BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN transcode_job TEXT; -- Transcoder job name, NULL until started

CREATE INDEX videos_lesson_id_idx ON videos (lesson_id);
