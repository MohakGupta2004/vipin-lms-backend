-- Single-choice / multiple-choice only for now.
CREATE TABLE questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    quiz_id UUID NOT NULL REFERENCES quizzes(id) ON DELETE CASCADE,

    question_text TEXT NOT NULL,
    explanation TEXT, -- shown after answering
    position INT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX questions_quiz_id_idx ON questions (quiz_id);

CREATE TRIGGER questions_set_updated_at
    BEFORE UPDATE ON questions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Never send is_correct to the client before submission.
CREATE TABLE question_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,

    option_text TEXT NOT NULL,
    is_correct BOOLEAN NOT NULL DEFAULT false,
    position INT NOT NULL
);

CREATE INDEX question_options_question_id_idx ON question_options (question_id);
