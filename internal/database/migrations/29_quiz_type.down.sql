DROP INDEX IF EXISTS quizzes_lesson_id_type_idx;
ALTER TABLE quizzes DROP CONSTRAINT IF EXISTS quizzes_practice_untimed_unscored;
UPDATE quizzes SET pass_percent = 70 WHERE pass_percent IS NULL;
ALTER TABLE quizzes ALTER COLUMN pass_percent SET NOT NULL;
ALTER TABLE quizzes DROP COLUMN type;
