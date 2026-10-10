-- mock_test is timed and scored; practice is untimed with no marks, answers still explained.
ALTER TABLE quizzes
    ADD COLUMN type TEXT NOT NULL DEFAULT 'mock_test' CHECK (type IN ('mock_test', 'practice'));
ALTER TABLE quizzes ALTER COLUMN pass_percent DROP NOT NULL;
ALTER TABLE quizzes ADD CONSTRAINT quizzes_practice_untimed_unscored CHECK (
    (type = 'mock_test' AND pass_percent IS NOT NULL)
    OR (type = 'practice' AND time_limit_sec IS NULL AND pass_percent IS NULL)
);
CREATE INDEX quizzes_lesson_id_type_idx ON quizzes (lesson_id, type);
