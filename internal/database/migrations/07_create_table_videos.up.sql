-- One row per uploaded video file. Upload/transcode lifecycle lives here, not on lessons.
CREATE TABLE videos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    uploaded_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    course_id UUID REFERENCES courses(id) ON DELETE SET NULL,

    title TEXT NOT NULL,
    original_key TEXT NOT NULL, -- raw upload in GCS
    hls_prefix TEXT,            -- transcoded HLS folder, NULL until ready
    duration_sec INT,
    size_bytes BIGINT,

    status TEXT NOT NULL DEFAULT 'uploading' CHECK (status IN ('uploading', 'processing', 'ready', 'failed')),
    error TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX videos_uploaded_by_idx ON videos (uploaded_by);
CREATE INDEX videos_course_id_idx ON videos (course_id);

CREATE TRIGGER videos_set_updated_at
    BEFORE UPDATE ON videos
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
