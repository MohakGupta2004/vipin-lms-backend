-- Tracks a PDF upload being parsed into a draft quiz.
CREATE TABLE quiz_imports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    uploaded_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    course_id UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    quiz_id UUID REFERENCES quizzes(id) ON DELETE SET NULL, -- draft quiz that was created

    pdf_key TEXT NOT NULL, -- GCS object key
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    error TEXT,
    question_count INT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX quiz_imports_uploaded_by_idx ON quiz_imports (uploaded_by);
CREATE INDEX quiz_imports_course_id_idx ON quiz_imports (course_id);
CREATE INDEX quiz_imports_quiz_id_idx ON quiz_imports (quiz_id);

CREATE TRIGGER quiz_imports_set_updated_at
    BEFORE UPDATE ON quiz_imports
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
