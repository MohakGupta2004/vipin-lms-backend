-- Deleting a quiz hides it instead of removing the row, so students' attempts and scores are kept.
ALTER TABLE quizzes ADD COLUMN deleted_at TIMESTAMPTZ;
