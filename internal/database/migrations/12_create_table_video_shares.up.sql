-- Instructor shares a video with one student or a whole course, for a limited time.
CREATE TABLE video_shares (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    video_id UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    shared_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    course_id UUID REFERENCES courses(id) ON DELETE CASCADE,

    message TEXT,
    expires_at TIMESTAMPTZ, -- NULL = never

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (user_id IS NOT NULL OR course_id IS NOT NULL)
);

CREATE INDEX video_shares_video_id_idx ON video_shares (video_id);
CREATE INDEX video_shares_shared_by_idx ON video_shares (shared_by);
CREATE INDEX video_shares_user_id_idx ON video_shares (user_id);
CREATE INDEX video_shares_course_id_idx ON video_shares (course_id);
