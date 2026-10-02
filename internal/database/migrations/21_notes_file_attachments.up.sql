-- A note can now carry a PDF. The file lives in the GCS bucket; object_key points to it.
-- Instructors upload PDFs as visibility = 'course' notes attached to a lesson.
-- Text notes keep these three columns NULL; either all three are set or none.
ALTER TABLE notes
    ADD COLUMN file_name TEXT,
    ADD COLUMN object_key TEXT UNIQUE,
    ADD COLUMN size_bytes BIGINT CHECK (size_bytes > 0),
    ADD CONSTRAINT notes_file_columns_check CHECK (
        (object_key IS NULL AND file_name IS NULL AND size_bytes IS NULL)
        OR (object_key IS NOT NULL AND file_name IS NOT NULL AND size_bytes IS NOT NULL)
    );
