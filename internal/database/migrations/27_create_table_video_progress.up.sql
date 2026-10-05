-- Where a user stopped in a video, so playback can resume there. Upserted on every progress ping.
CREATE TABLE video_progress (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,

    position_sec INT NOT NULL DEFAULT 0 CHECK (position_sec >= 0),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, video_id)
);

-- user_id lookups use the primary key; this one serves ON DELETE CASCADE from videos.
CREATE INDEX video_progress_video_id_idx ON video_progress (video_id);

CREATE TRIGGER video_progress_set_updated_at
    BEFORE UPDATE ON video_progress
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
