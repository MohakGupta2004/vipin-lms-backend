-- Each note, quiz, video and flashcard lists the tiers that can open it. ultra is implied and never stored;
-- an empty array means ultra only. 'free' means everyone, enrolled or not. is_free stays as a read-only column.

ALTER TABLE notes ADD COLUMN tiers TEXT[] NOT NULL DEFAULT '{}'
    CONSTRAINT notes_tiers_check CHECK (tiers <@ ARRAY['free','plus','premium']);
UPDATE notes SET tiers = CASE WHEN is_free THEN ARRAY['free'] ELSE ARRAY['plus','premium'] END;
ALTER TABLE notes ALTER COLUMN tiers DROP DEFAULT;
ALTER TABLE notes DROP COLUMN is_free;
ALTER TABLE notes ADD COLUMN is_free BOOLEAN GENERATED ALWAYS AS ('free' = ANY(tiers)) STORED;

ALTER TABLE quizzes ADD COLUMN tiers TEXT[] NOT NULL DEFAULT '{}'
    CONSTRAINT quizzes_tiers_check CHECK (tiers <@ ARRAY['free','plus','premium']);
UPDATE quizzes SET tiers = CASE WHEN is_free THEN ARRAY['free'] ELSE ARRAY['plus','premium'] END;
ALTER TABLE quizzes ALTER COLUMN tiers DROP DEFAULT;
ALTER TABLE quizzes DROP COLUMN is_free;
ALTER TABLE quizzes ADD COLUMN is_free BOOLEAN GENERATED ALWAYS AS ('free' = ANY(tiers)) STORED;

ALTER TABLE flashcards ADD COLUMN tiers TEXT[] NOT NULL DEFAULT '{}'
    CONSTRAINT flashcards_tiers_check CHECK (tiers <@ ARRAY['free','plus','premium']);
UPDATE flashcards SET tiers = CASE WHEN is_free THEN ARRAY['free'] ELSE ARRAY['plus','premium'] END;
ALTER TABLE flashcards ALTER COLUMN tiers DROP DEFAULT;
ALTER TABLE flashcards DROP COLUMN is_free;
ALTER TABLE flashcards ADD COLUMN is_free BOOLEAN GENERATED ALWAYS AS ('free' = ANY(tiers)) STORED;

ALTER TABLE videos ADD COLUMN tiers TEXT[] NOT NULL DEFAULT '{}'
    CONSTRAINT videos_tiers_check CHECK (tiers <@ ARRAY['free','plus','premium']);
UPDATE videos SET tiers = CASE WHEN is_free THEN ARRAY['free'] ELSE ARRAY['premium'] END;
ALTER TABLE videos ALTER COLUMN tiers DROP DEFAULT;
ALTER TABLE videos DROP COLUMN is_free;
ALTER TABLE videos ADD COLUMN is_free BOOLEAN GENERATED ALWAYS AS ('free' = ANY(tiers)) STORED;
