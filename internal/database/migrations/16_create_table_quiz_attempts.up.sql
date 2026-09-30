CREATE TABLE quiz_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    quiz_id UUID NOT NULL REFERENCES quizzes(id) ON DELETE CASCADE,

    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    submitted_at TIMESTAMPTZ,
    score INT, -- correct answers
    total INT, -- questions at time of attempt
    passed BOOLEAN
);

-- user_id is covered by the (user_id, quiz_id) index.
CREATE INDEX quiz_attempts_user_id_quiz_id_idx ON quiz_attempts (user_id, quiz_id);
CREATE INDEX quiz_attempts_quiz_id_idx ON quiz_attempts (quiz_id);

CREATE TABLE attempt_answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    attempt_id UUID NOT NULL REFERENCES quiz_attempts(id) ON DELETE CASCADE,
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    selected_option_id UUID REFERENCES question_options(id) ON DELETE SET NULL,

    is_correct BOOLEAN NOT NULL DEFAULT false,

    UNIQUE (attempt_id, question_id)
);

-- attempt_id is covered by the unique (attempt_id, question_id) index.
CREATE INDEX attempt_answers_question_id_idx ON attempt_answers (question_id);
CREATE INDEX attempt_answers_selected_option_id_idx ON attempt_answers (selected_option_id);
