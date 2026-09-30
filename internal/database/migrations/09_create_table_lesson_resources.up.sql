-- Attachments: PDFs, slides, ZIPs, study material.
CREATE TABLE lesson_resources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    lesson_id UUID NOT NULL REFERENCES lessons(id) ON DELETE CASCADE,

    name TEXT NOT NULL,
    object_key TEXT NOT NULL, -- GCS object key
    mime_type TEXT,
    size_bytes BIGINT,
    position INT NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX lesson_resources_lesson_id_idx ON lesson_resources (lesson_id);
