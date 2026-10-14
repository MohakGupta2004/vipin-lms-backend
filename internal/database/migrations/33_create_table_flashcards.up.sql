-- A deck is the set of flashcards of one lesson; lesson_id NULL is the course-wide deck.
-- Publishing is per deck, free preview per card.
CREATE TABLE flashcard_decks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    course_id UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    lesson_id UUID REFERENCES lessons(id) ON DELETE CASCADE,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,

    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

-- One live deck per lesson, and one course-wide deck per course.
CREATE UNIQUE INDEX flashcard_decks_course_lesson_key ON flashcard_decks (course_id, lesson_id) NULLS NOT DISTINCT
    WHERE deleted_at IS NULL;
CREATE INDEX flashcard_decks_lesson_id_idx ON flashcard_decks (lesson_id);
CREATE INDEX flashcard_decks_created_by_idx ON flashcard_decks (created_by);

CREATE TRIGGER flashcard_decks_set_updated_at
    BEFORE UPDATE ON flashcard_decks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE flashcards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    deck_id UUID NOT NULL REFERENCES flashcard_decks(id) ON DELETE CASCADE,
    course_id UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE, -- same as the deck's, for one-join access checks
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,

    front TEXT NOT NULL,
    back TEXT NOT NULL,
    is_free BOOLEAN NOT NULL DEFAULT false,
    position INT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX flashcards_deck_id_idx ON flashcards (deck_id);
CREATE INDEX flashcards_course_id_idx ON flashcards (course_id);
CREATE INDEX flashcards_created_by_idx ON flashcards (created_by);

CREATE TRIGGER flashcards_set_updated_at
    BEFORE UPDATE ON flashcards
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A student's latest self-rating of a card (1 = no idea, 5 = perfect). Lower ratings make a card more likely in mixes.
CREATE TABLE flashcard_reviews (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    flashcard_id UUID NOT NULL REFERENCES flashcards(id) ON DELETE CASCADE,

    rating SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    review_count INT NOT NULL DEFAULT 1,
    last_reviewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, flashcard_id)
);

-- user_id lookups use the primary key; this one serves ON DELETE CASCADE from flashcards.
CREATE INDEX flashcard_reviews_flashcard_id_idx ON flashcard_reviews (flashcard_id);

CREATE TRIGGER flashcard_reviews_set_updated_at
    BEFORE UPDATE ON flashcard_reviews
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
