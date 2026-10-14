package models

import (
	"context"
	"database/sql"
)

// FlashcardDeck is the set of flashcards of one lesson, or of a whole course when LessonID is nil.
// The counts with "My" are the asking user's own review progress.
type FlashcardDeck struct {
	ID            string  `json:"id"`
	CourseID      string  `json:"courseId"`
	LessonID      *string `json:"lessonId"` // nil = course-wide deck
	LessonTitle   string  `json:"lessonTitle,omitempty"`
	CourseTitle   string  `json:"courseTitle"`
	Status        string  `json:"status"` // draft or published
	CardCount     int     `json:"cardCount"`
	FreeCardCount int     `json:"freeCardCount"`
	MyReviewed    int     `json:"myReviewed"`
	MyMastered    int     `json:"myMastered"` // cards rated 4 or 5

	LessonPublished bool `json:"-"` // students may only see decks of published lessons
}

// Flashcard is one front/back card of a deck. Front and Back are blank when Locked.
type Flashcard struct {
	ID       string `json:"id"`
	DeckID   string `json:"deckId"`
	CourseID string `json:"courseId"`
	Front    string `json:"front"`
	Back     string `json:"back"`
	IsFree   bool   `json:"isFree"`
	Position int    `json:"position"`
	MyRating *int   `json:"myRating"` // nil = never rated
	// Locked is set for users previewing a course: they see the card exists but cannot study it.
	Locked bool `json:"locked,omitempty"`
}

type FlashcardRepository struct {
	db *sql.DB
}

func NewFlashcardRepository(db *sql.DB) *FlashcardRepository {
	return &FlashcardRepository{db: db}
}

// deckColumns are the columns scanDeck reads. $1 must be the id of the user whose progress is counted.
const deckColumns = `d.id, d.course_id, d.lesson_id, COALESCE(l.title, ''), c.title, d.status,
		(SELECT COUNT(*) FROM flashcards f WHERE f.deck_id = d.id AND f.deleted_at IS NULL),
		(SELECT COUNT(*) FROM flashcards f WHERE f.deck_id = d.id AND f.deleted_at IS NULL AND f.is_free),
		(SELECT COUNT(*) FROM flashcards f JOIN flashcard_reviews r ON r.flashcard_id = f.id AND r.user_id = $1
			WHERE f.deck_id = d.id AND f.deleted_at IS NULL),
		(SELECT COUNT(*) FROM flashcards f JOIN flashcard_reviews r ON r.flashcard_id = f.id AND r.user_id = $1
			WHERE f.deck_id = d.id AND f.deleted_at IS NULL AND r.rating >= 4),
		COALESCE(l.is_published, d.lesson_id IS NULL)`

// deckFrom joins a deck (d) to its course (c) and its lesson (l, absent for a course-wide deck).
// A deck whose lesson was deleted is left out.
const deckFrom = `FROM flashcard_decks d
		JOIN courses c ON c.id = d.course_id AND c.deleted_at IS NULL
		LEFT JOIN lessons l ON l.id = d.lesson_id AND l.deleted_at IS NULL`

const deckLive = `d.deleted_at IS NULL AND (d.lesson_id IS NULL OR l.id IS NOT NULL)`

const deckSelect = `SELECT ` + deckColumns + ` ` + deckFrom + ` WHERE ` + deckLive

func scanDeck(row interface{ Scan(...any) error }, d *FlashcardDeck) error {
	var lessonID sql.NullString
	err := row.Scan(&d.ID, &d.CourseID, &lessonID, &d.LessonTitle, &d.CourseTitle, &d.Status,
		&d.CardCount, &d.FreeCardCount, &d.MyReviewed, &d.MyMastered, &d.LessonPublished)
	if err != nil {
		return err
	}
	if lessonID.Valid {
		d.LessonID = &lessonID.String
	}
	return nil
}

// cardColumns are the columns scanCard reads, from flashcards aliased f with the user's review as r.
const cardColumns = `f.id, f.deck_id, f.course_id, f.front, f.back, f.is_free, f.position, r.rating`

// cardFrom joins a card to the review of user $1.
const cardFrom = `FROM flashcards f
		LEFT JOIN flashcard_reviews r ON r.flashcard_id = f.id AND r.user_id = $1`

func scanCard(row interface{ Scan(...any) error }, f *Flashcard) error {
	var rating sql.NullInt32
	if err := row.Scan(&f.ID, &f.DeckID, &f.CourseID, &f.Front, &f.Back, &f.IsFree, &f.Position, &rating); err != nil {
		return err
	}
	if rating.Valid {
		v := int(rating.Int32)
		f.MyRating = &v
	}
	return nil
}

// sessionOrder is a weighted random order without repeats (Efraimidis-Spirakis): each card gets the key
// -ln(u)/weight and the smallest keys come first. A card rated 1 weighs 5, rated 5 weighs 1, unrated 3,
// so low-rated cards come up about five times as often. 1 - random() is never 0, so ln() is always defined.
const sessionOrder = `-ln(1 - random()) / (CASE WHEN r.rating IS NULL THEN 3 ELSE 6 - r.rating END)::float8`

// CreateDeck saves an empty deck and fills in the generated fields. It returns a unique violation if the
// lesson (or the course, for a course-wide deck) already has a deck.
func (r *FlashcardRepository) CreateDeck(ctx context.Context, courseID string, lessonID *string, createdBy, status string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `INSERT INTO flashcard_decks (course_id, lesson_id, created_by, status)
		VALUES ($1, $2, $3, $4) RETURNING id`, courseID, lessonID, createdBy, status).Scan(&id)
	return id, err
}

// GetDeck returns one deck with the user's progress, or sql.ErrNoRows.
func (r *FlashcardRepository) GetDeck(ctx context.Context, deckID, userID string) (*FlashcardDeck, error) {
	var d FlashcardDeck
	if err := scanDeck(r.db.QueryRowContext(ctx, deckSelect+" AND d.id = $2", userID, deckID), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// GetDeckByLesson returns the deck of a lesson (lessonID empty = the course-wide deck), or sql.ErrNoRows.
func (r *FlashcardRepository) GetDeckByLesson(ctx context.Context, courseID, lessonID, userID string) (*FlashcardDeck, error) {
	var d FlashcardDeck
	query := deckSelect + " AND d.course_id = $2 AND d.lesson_id IS NOT DISTINCT FROM $3::uuid"
	if err := scanDeck(r.db.QueryRowContext(ctx, query, userID, courseID, nullUUID(lessonID)), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// ListCourseDecks returns a course's decks in lesson order, the course-wide deck last. Learners only get
// published decks of published lessons; includeDrafts lifts that for the owner.
func (r *FlashcardRepository) ListCourseDecks(ctx context.Context, courseID, userID string, includeDrafts bool) ([]FlashcardDeck, error) {
	query := deckSelect + `
		AND d.course_id = $2
		AND ($3 OR (d.status = 'published' AND COALESCE(l.is_published, true)))
		ORDER BY l.position NULLS LAST, d.created_at, d.id`
	return r.queryDecks(ctx, query, userID, courseID, includeDrafts)
}

// ListMyDecks returns the published decks with cards in the courses the user is enrolled in
// (one course if courseID is set), in course and lesson order.
func (r *FlashcardRepository) ListMyDecks(ctx context.Context, userID, courseID string) ([]FlashcardDeck, error) {
	query := myCourses + `
		SELECT ` + deckColumns + ` ` + deckFrom + `
		JOIN mc ON mc.id = d.course_id
		WHERE ` + deckLive + `
		  AND d.status = 'published' AND COALESCE(l.is_published, true)
		  AND ($3::uuid IS NULL OR d.course_id = $3::uuid)
		  AND EXISTS (SELECT 1 FROM flashcards f WHERE f.deck_id = d.id AND f.deleted_at IS NULL)
		ORDER BY c.title, c.id, l.position NULLS LAST, d.created_at, d.id`
	return r.queryDecks(ctx, query, userID, false, nullUUID(courseID))
}

// RecentDeck returns the published deck of the lesson the user touched last, by watching its video or
// rating one of its cards, among the courses they are enrolled in. It returns sql.ErrNoRows if there is none.
func (r *FlashcardRepository) RecentDeck(ctx context.Context, userID string) (*FlashcardDeck, error) {
	query := myCourses + `,
	touched AS (
		SELECT v.lesson_id, MAX(vp.updated_at) AS at
		FROM video_progress vp
		JOIN videos v ON v.id = vp.video_id
		WHERE vp.user_id = $1 AND v.lesson_id IS NOT NULL
		GROUP BY v.lesson_id
		UNION ALL
		SELECT d.lesson_id, MAX(r.last_reviewed_at)
		FROM flashcard_reviews r
		JOIN flashcards f ON f.id = r.flashcard_id
		JOIN flashcard_decks d ON d.id = f.deck_id
		WHERE r.user_id = $1 AND d.lesson_id IS NOT NULL
		GROUP BY d.lesson_id
	)
		SELECT ` + deckColumns + ` ` + deckFrom + `
		JOIN mc ON mc.id = d.course_id
		JOIN touched t ON t.lesson_id = d.lesson_id
		WHERE ` + deckLive + `
		  AND d.status = 'published' AND l.is_published
		  AND EXISTS (SELECT 1 FROM flashcards f WHERE f.deck_id = d.id AND f.deleted_at IS NULL)
		ORDER BY t.at DESC
		LIMIT 1`
	var d FlashcardDeck
	if err := scanDeck(r.db.QueryRowContext(ctx, query, userID, false), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *FlashcardRepository) queryDecks(ctx context.Context, query string, args ...any) ([]FlashcardDeck, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	decks := []FlashcardDeck{}
	for rows.Next() {
		var d FlashcardDeck
		if err := scanDeck(rows, &d); err != nil {
			return nil, err
		}
		decks = append(decks, d)
	}
	return decks, rows.Err()
}

// UpdateDeckStatus changes a deck's status. It returns sql.ErrNoRows if the deck does not exist.
func (r *FlashcardRepository) UpdateDeckStatus(ctx context.Context, deckID, status string) error {
	var id string
	return r.db.QueryRowContext(ctx, "UPDATE flashcard_decks SET status = $1 WHERE id = $2 AND deleted_at IS NULL RETURNING id",
		status, deckID).Scan(&id)
}

// SetDeckFree marks every card of the deck free or paid.
func (r *FlashcardRepository) SetDeckFree(ctx context.Context, deckID string, isFree bool) error {
	_, err := r.db.ExecContext(ctx, "UPDATE flashcards SET is_free = $2 WHERE deck_id = $1 AND deleted_at IS NULL", deckID, isFree)
	return err
}

// DeleteDeck soft-deletes a deck and its cards. Reviews are kept.
func (r *FlashcardRepository) DeleteDeck(ctx context.Context, deckID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id string
	err = tx.QueryRowContext(ctx, "UPDATE flashcard_decks SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING id", deckID).Scan(&id)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE flashcards SET deleted_at = now() WHERE deck_id = $1 AND deleted_at IS NULL", deckID); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateCard saves a card at the end of its deck and fills in the generated fields.
func (r *FlashcardRepository) CreateCard(ctx context.Context, f *Flashcard, createdBy string) error {
	query := `INSERT INTO flashcards (deck_id, course_id, created_by, front, back, is_free, position)
		VALUES ($1, $2, $3, $4, $5, $6,
		        (SELECT COALESCE(MAX(position), 0) + 1 FROM flashcards WHERE deck_id = $1 AND deleted_at IS NULL))
		RETURNING id, position`
	return r.db.QueryRowContext(ctx, query, f.DeckID, f.CourseID, createdBy, f.Front, f.Back, f.IsFree).Scan(&f.ID, &f.Position)
}

// GetCard returns one card with the user's rating, or sql.ErrNoRows.
func (r *FlashcardRepository) GetCard(ctx context.Context, cardID, userID string) (*Flashcard, error) {
	var f Flashcard
	query := `SELECT ` + cardColumns + ` ` + cardFrom + ` WHERE f.deleted_at IS NULL AND f.id = $2`
	if err := scanCard(r.db.QueryRowContext(ctx, query, userID, cardID), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ListDeckCards returns a deck's cards in order, each with the user's rating.
func (r *FlashcardRepository) ListDeckCards(ctx context.Context, deckID, userID string) ([]Flashcard, error) {
	query := `SELECT ` + cardColumns + ` ` + cardFrom + `
		WHERE f.deleted_at IS NULL AND f.deck_id = $2
		ORDER BY f.position, f.id`
	return r.queryCards(ctx, query, userID, deckID)
}

// SessionForDeck returns up to limit distinct cards of a deck in weighted random order (see sessionOrder).
// freeOnly keeps just the free cards.
func (r *FlashcardRepository) SessionForDeck(ctx context.Context, deckID, userID string, freeOnly bool, limit int) ([]Flashcard, error) {
	query := `SELECT ` + cardColumns + ` ` + cardFrom + `
		WHERE f.deleted_at IS NULL AND f.deck_id = $2 AND (NOT $3 OR f.is_free)
		ORDER BY ` + sessionOrder + `
		LIMIT $4`
	return r.queryCards(ctx, query, userID, deckID, freeOnly, limit)
}

// SessionMix returns up to limit distinct cards from the published decks of the courses the user is
// enrolled in (one course if courseID is set), in weighted random order.
func (r *FlashcardRepository) SessionMix(ctx context.Context, userID, courseID string, limit int) ([]Flashcard, error) {
	query := myCourses + `
		SELECT ` + cardColumns + ` ` + cardFrom + `
		JOIN flashcard_decks d ON d.id = f.deck_id AND d.deleted_at IS NULL AND d.status = 'published'
		JOIN mc ON mc.id = f.course_id
		LEFT JOIN lessons l ON l.id = d.lesson_id AND l.deleted_at IS NULL
		WHERE f.deleted_at IS NULL
		  AND (d.lesson_id IS NULL OR l.is_published)
		  AND ($3::uuid IS NULL OR f.course_id = $3::uuid)
		ORDER BY ` + sessionOrder + `
		LIMIT $4`
	return r.queryCards(ctx, query, userID, false, nullUUID(courseID), limit)
}

func (r *FlashcardRepository) queryCards(ctx context.Context, query string, args ...any) ([]Flashcard, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cards := []Flashcard{}
	for rows.Next() {
		var f Flashcard
		if err := scanCard(rows, &f); err != nil {
			return nil, err
		}
		cards = append(cards, f)
	}
	return cards, rows.Err()
}

// UpdateCard saves a card's front, back and free flag. It returns sql.ErrNoRows if the card does not exist.
func (r *FlashcardRepository) UpdateCard(ctx context.Context, f *Flashcard) error {
	var id string
	return r.db.QueryRowContext(ctx, `UPDATE flashcards SET front = $1, back = $2, is_free = $3
		WHERE id = $4 AND deleted_at IS NULL RETURNING id`, f.Front, f.Back, f.IsFree, f.ID).Scan(&id)
}

// DeleteCard soft-deletes a card. It returns sql.ErrNoRows if the card does not exist.
func (r *FlashcardRepository) DeleteCard(ctx context.Context, cardID string) error {
	var id string
	return r.db.QueryRowContext(ctx, "UPDATE flashcards SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING id", cardID).Scan(&id)
}

// UpsertReview stores the user's latest rating of a card and counts the review.
func (r *FlashcardRepository) UpsertReview(ctx context.Context, userID, cardID string, rating int) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO flashcard_reviews (user_id, flashcard_id, rating)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, flashcard_id) DO UPDATE SET
			rating = EXCLUDED.rating,
			review_count = flashcard_reviews.review_count + 1,
			last_reviewed_at = now()`, userID, cardID, rating)
	return err
}
