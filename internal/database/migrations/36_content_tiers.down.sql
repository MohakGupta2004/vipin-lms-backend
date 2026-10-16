ALTER TABLE notes DROP COLUMN is_free;
ALTER TABLE notes ADD COLUMN is_free BOOLEAN NOT NULL DEFAULT false;
UPDATE notes SET is_free = 'free' = ANY(tiers);
ALTER TABLE notes DROP COLUMN tiers;

ALTER TABLE quizzes DROP COLUMN is_free;
ALTER TABLE quizzes ADD COLUMN is_free BOOLEAN NOT NULL DEFAULT false;
UPDATE quizzes SET is_free = 'free' = ANY(tiers);
ALTER TABLE quizzes DROP COLUMN tiers;

ALTER TABLE flashcards DROP COLUMN is_free;
ALTER TABLE flashcards ADD COLUMN is_free BOOLEAN NOT NULL DEFAULT false;
UPDATE flashcards SET is_free = 'free' = ANY(tiers);
ALTER TABLE flashcards DROP COLUMN tiers;

ALTER TABLE videos DROP COLUMN is_free;
ALTER TABLE videos ADD COLUMN is_free BOOLEAN NOT NULL DEFAULT false;
UPDATE videos SET is_free = 'free' = ANY(tiers);
ALTER TABLE videos DROP COLUMN tiers;
