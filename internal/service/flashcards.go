package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

const (
	maxFlashcardFrontLength = 500
	maxFlashcardBackLength  = 2000
	defaultSessionLimit     = 20
	maxSessionLimit         = 100
)

var (
	// ErrDeckNotFound means the deck does not exist, or the user may not see it.
	ErrDeckNotFound = errors.New("flashcard deck not found")
	// ErrFlashcardNotFound means the card does not exist, or the user may not see it.
	ErrFlashcardNotFound = errors.New("flashcard not found")
	// ErrDeckExists means the lesson (or the course, for a course-wide deck) already has a deck.
	ErrDeckExists = errors.New("this lesson already has a flashcard deck")
)

// CreateDeckInput is what an instructor sends to create a deck. A nil LessonID makes the course-wide deck.
type CreateDeckInput struct {
	LessonID *string
	Status   string // "" = draft
}

type CreateCardInput struct {
	Front  string
	Back   string
	Tiers  []string // nil = plus and premium; ultra is always included
	IsFree bool     // deprecated: true is the same as tiers [free]
}

// UpdateCardInput holds the card fields to change. Nil fields are left as they are.
type UpdateCardInput struct {
	Front  *string
	Back   *string
	Tiers  []string // nil = unchanged
	IsFree *bool    // deprecated: true adds the free tier, false removes it
}

// MyFlashcards is what the dashboard and the hub show a student: the deck of the lesson they studied
// last (nil if none) and all their decks.
type MyFlashcards struct {
	Recent *models.FlashcardDeck  `json:"recent"`
	Decks  []models.FlashcardDeck `json:"decks"`
}

type FlashcardService struct {
	repo       *models.FlashcardRepository
	lessonRepo *models.LessonRepository
}

func NewFlashcardService(repo *models.FlashcardRepository, lessonRepo *models.LessonRepository) *FlashcardService {
	return &FlashcardService{repo: repo, lessonRepo: lessonRepo}
}

func validateCardText(front, back string) error {
	switch {
	case front == "":
		return fmt.Errorf("%w: front is required", ErrInvalidInput)
	case utf8.RuneCountInString(front) > maxFlashcardFrontLength:
		return fmt.Errorf("%w: front must be at most %d characters", ErrInvalidInput, maxFlashcardFrontLength)
	case back == "":
		return fmt.Errorf("%w: back is required", ErrInvalidInput)
	case utf8.RuneCountInString(back) > maxFlashcardBackLength:
		return fmt.Errorf("%w: back must be at most %d characters", ErrInvalidInput, maxFlashcardBackLength)
	}
	return nil
}

func validateDeckStatus(status string) error {
	if status != "draft" && status != "published" {
		return fmt.Errorf("%w: status must be draft or published", ErrInvalidInput)
	}
	return nil
}

func validateRating(rating int) error {
	if rating < 1 || rating > 5 {
		return fmt.Errorf("%w: rating must be between 1 and 5", ErrInvalidInput)
	}
	return nil
}

// sessionLimit applies the default and the cap to a requested session size.
func sessionLimit(limit int) (int, error) {
	switch {
	case limit == 0:
		return defaultSessionLimit, nil
	case limit < 0:
		return 0, fmt.Errorf("%w: limit must be positive", ErrInvalidInput)
	case limit > maxSessionLimit:
		return maxSessionLimit, nil
	}
	return limit, nil
}

// ListCourseDecks returns a course's decks with the user's progress. The owner sees drafts too.
func (s *FlashcardService) ListCourseDecks(ctx context.Context, user *models.User, courseID string) ([]models.FlashcardDeck, error) {
	isTeacher, _, err := authorizeTier(ctx, s.lessonRepo, user, courseID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListCourseDecks(ctx, courseID, user.ID, isTeacher)
}

// CreateDeck lets the course owner add the deck of a lesson, or the course-wide deck when no lesson is given.
func (s *FlashcardService) CreateDeck(ctx context.Context, user *models.User, courseID string, in CreateDeckInput) (*models.FlashcardDeck, error) {
	if in.Status == "" {
		in.Status = "draft"
	}
	if err := validateDeckStatus(in.Status); err != nil {
		return nil, err
	}
	if in.LessonID != nil {
		id := strings.ToLower(strings.TrimSpace(*in.LessonID))
		if id == "" {
			in.LessonID = nil
		} else if !uuidPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: lessonId must be a UUID", ErrInvalidInput)
		} else {
			in.LessonID = &id
		}
	}

	if err := requireTeacher(ctx, s.lessonRepo, user, courseID); err != nil {
		return nil, err
	}
	if in.LessonID != nil {
		lesson, err := s.lessonRepo.GetLesson(ctx, *in.LessonID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && lesson.CourseID != courseID) {
			return nil, ErrLessonNotFound
		}
		if err != nil {
			return nil, err
		}
	}

	id, err := s.repo.CreateDeck(ctx, courseID, in.LessonID, user.ID, in.Status)
	if isUniqueViolation(err) {
		return nil, ErrDeckExists
	}
	if err != nil {
		return nil, err
	}
	return s.repo.GetDeck(ctx, id, user.ID)
}

// authorizeDeck loads a deck the user may see: the course owner sees every deck, anyone else only a
// published deck of a published lesson. tier decides which of its cards the user may study.
func (s *FlashcardService) authorizeDeck(ctx context.Context, user *models.User, deckID string) (deck *models.FlashcardDeck, isTeacher bool, tier models.Tier, err error) {
	if !uuidPattern.MatchString(deckID) {
		return nil, false, "", ErrDeckNotFound
	}
	deck, err = s.repo.GetDeck(ctx, deckID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, "", ErrDeckNotFound
	}
	if err != nil {
		return nil, false, "", err
	}
	isTeacher, tier, err = authorizeTier(ctx, s.lessonRepo, user, deck.CourseID)
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
		return nil, false, "", ErrDeckNotFound
	}
	if err != nil {
		return nil, false, "", err
	}
	if !isTeacher && (deck.Status != "published" || !deck.LessonPublished) {
		return nil, false, "", ErrDeckNotFound
	}
	return deck, isTeacher, tier, nil
}

// requireDeckOwner loads a deck for its course owner, who is the only one allowed to change it.
func (s *FlashcardService) requireDeckOwner(ctx context.Context, user *models.User, deckID string) (*models.FlashcardDeck, error) {
	deck, isTeacher, _, err := s.authorizeDeck(ctx, user, deckID)
	if err != nil {
		return nil, err
	}
	if !isTeacher {
		return nil, ErrForbidden
	}
	return deck, nil
}

// UpdateDeckStatus lets the course owner publish a deck or turn it back into a draft.
func (s *FlashcardService) UpdateDeckStatus(ctx context.Context, user *models.User, deckID, status string) (*models.FlashcardDeck, error) {
	if err := validateDeckStatus(status); err != nil {
		return nil, err
	}
	deck, err := s.requireDeckOwner(ctx, user, deckID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateDeckStatus(ctx, deck.ID, status); err != nil {
		return nil, mapDeckErr(err)
	}
	return s.repo.GetDeck(ctx, deck.ID, user.ID)
}

// SetDeckTiers lets the course owner give every card of a deck the same tiers in one go.
func (s *FlashcardService) SetDeckTiers(ctx context.Context, user *models.User, deckID string, tiers []string) (*models.FlashcardDeck, error) {
	tiers, err := normalizeTiers(tiers, defaultContentTiers)
	if err != nil {
		return nil, err
	}
	deck, err := s.requireDeckOwner(ctx, user, deckID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetDeckTiers(ctx, deck.ID, tiers); err != nil {
		return nil, err
	}
	return s.repo.GetDeck(ctx, deck.ID, user.ID)
}

// SetDeckFree is the deprecated form of SetDeckTiers: free cards are open to everyone, paid ones to
// the default paid tiers.
func (s *FlashcardService) SetDeckFree(ctx context.Context, user *models.User, deckID string, isFree bool) (*models.FlashcardDeck, error) {
	if isFree {
		return s.SetDeckTiers(ctx, user, deckID, []string{string(models.TierFree)})
	}
	return s.SetDeckTiers(ctx, user, deckID, nil)
}

// DeleteDeck lets the course owner delete a deck with all its cards.
func (s *FlashcardService) DeleteDeck(ctx context.Context, user *models.User, deckID string) error {
	deck, err := s.requireDeckOwner(ctx, user, deckID)
	if err != nil {
		return err
	}
	return mapDeckErr(s.repo.DeleteDeck(ctx, deck.ID))
}

func mapDeckErr(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrDeckNotFound
	}
	return err
}

// ListDeckCards returns a deck's cards in order with the user's ratings. Cards the user's tier cannot
// study come back locked, with front and back blanked.
func (s *FlashcardService) ListDeckCards(ctx context.Context, user *models.User, deckID string) ([]models.Flashcard, error) {
	deck, _, tier, err := s.authorizeDeck(ctx, user, deckID)
	if err != nil {
		return nil, err
	}
	cards, err := s.repo.ListDeckCards(ctx, deck.ID, user.ID)
	if err != nil {
		return nil, err
	}
	markLockedCards(cards, tier)
	return cards, nil
}

// markLockedCards locks the cards a user on this tier cannot study: every card not tagged for the tier
// or free. Their text is blanked so it never leaves the server.
func markLockedCards(cards []models.Flashcard, tier models.Tier) {
	for i := range cards {
		if !models.CanAccess(tier, cards[i].Tiers) {
			cards[i].Locked = true
			cards[i].Front = ""
			cards[i].Back = ""
		}
	}
}

// CreateCard lets the course owner add a card at the end of a deck.
func (s *FlashcardService) CreateCard(ctx context.Context, user *models.User, deckID string, in CreateCardInput) (*models.Flashcard, error) {
	in.Front = strings.TrimSpace(in.Front)
	in.Back = strings.TrimSpace(in.Back)
	if err := validateCardText(in.Front, in.Back); err != nil {
		return nil, err
	}
	deck, err := s.requireDeckOwner(ctx, user, deckID)
	if err != nil {
		return nil, err
	}
	tiers := in.Tiers
	if tiers == nil && in.IsFree {
		tiers = []string{string(models.TierFree)}
	}
	tiers, err = normalizeTiers(tiers, defaultContentTiers)
	if err != nil {
		return nil, err
	}
	card := &models.Flashcard{DeckID: deck.ID, CourseID: deck.CourseID, Front: in.Front, Back: in.Back, Tiers: tiers}
	if err := s.repo.CreateCard(ctx, card, user.ID); err != nil {
		return nil, err
	}
	return card, nil
}

// loadCard loads a card and its deck the user may see.
func (s *FlashcardService) loadCard(ctx context.Context, user *models.User, cardID string) (*models.Flashcard, *models.FlashcardDeck, bool, models.Tier, error) {
	if !uuidPattern.MatchString(cardID) {
		return nil, nil, false, "", ErrFlashcardNotFound
	}
	card, err := s.repo.GetCard(ctx, cardID, user.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, "", ErrFlashcardNotFound
	}
	if err != nil {
		return nil, nil, false, "", err
	}
	deck, isTeacher, tier, err := s.authorizeDeck(ctx, user, card.DeckID)
	if errors.Is(err, ErrDeckNotFound) {
		return nil, nil, false, "", ErrFlashcardNotFound
	}
	if err != nil {
		return nil, nil, false, "", err
	}
	return card, deck, isTeacher, tier, nil
}

// UpdateCard lets the course owner edit a card.
func (s *FlashcardService) UpdateCard(ctx context.Context, user *models.User, cardID string, in UpdateCardInput) (*models.Flashcard, error) {
	card, _, isTeacher, _, err := s.loadCard(ctx, user, cardID)
	if err != nil {
		return nil, err
	}
	if !isTeacher {
		return nil, ErrForbidden
	}
	if in.Front != nil {
		card.Front = strings.TrimSpace(*in.Front)
	}
	if in.Back != nil {
		card.Back = strings.TrimSpace(*in.Back)
	}
	switch {
	case in.Tiers != nil:
		if card.Tiers, err = normalizeTiers(in.Tiers, nil); err != nil {
			return nil, err
		}
	case in.IsFree != nil:
		card.Tiers = legacyFreeTiers(card.Tiers, *in.IsFree, defaultContentTiers)
	}
	if err := validateCardText(card.Front, card.Back); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateCard(ctx, card); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFlashcardNotFound
		}
		return nil, err
	}
	return card, nil
}

// DeleteCard lets the course owner delete a card.
func (s *FlashcardService) DeleteCard(ctx context.Context, user *models.User, cardID string) error {
	card, _, isTeacher, _, err := s.loadCard(ctx, user, cardID)
	if err != nil {
		return err
	}
	if !isTeacher {
		return ErrForbidden
	}
	err = s.repo.DeleteCard(ctx, card.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrFlashcardNotFound
	}
	return err
}

// DeckSession returns a random study order of a deck's cards, no card twice, the low-rated ones likelier.
// Only the cards the user's tier can study are included.
func (s *FlashcardService) DeckSession(ctx context.Context, user *models.User, deckID string, limit int) ([]models.Flashcard, error) {
	limit, err := sessionLimit(limit)
	if err != nil {
		return nil, err
	}
	deck, _, tier, err := s.authorizeDeck(ctx, user, deckID)
	if err != nil {
		return nil, err
	}
	return s.repo.SessionForDeck(ctx, deck.ID, user.ID, tier, limit)
}

// MyFlashcards returns a student's decks and the deck of the lesson they studied last.
func (s *FlashcardService) MyFlashcards(ctx context.Context, user *models.User, courseID string) (*MyFlashcards, error) {
	if user.Role != models.RoleStudent {
		return nil, ErrForbidden
	}
	if err := validateOptionalID(courseID); err != nil {
		return nil, err
	}
	decks, err := s.repo.ListMyDecks(ctx, user.ID, courseID)
	if err != nil {
		return nil, err
	}
	out := &MyFlashcards{Decks: decks}
	recent, err := s.repo.RecentDeck(ctx, user.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// Respect the course filter: the recent deck must be one of the listed decks.
	if err == nil {
		for _, d := range decks {
			if d.ID == recent.ID {
				out.Recent = recent
				break
			}
		}
	}
	return out, nil
}

// MixSession returns a random study order across the student's courses (or one of them).
func (s *FlashcardService) MixSession(ctx context.Context, user *models.User, courseID string, limit int) ([]models.Flashcard, error) {
	if user.Role != models.RoleStudent {
		return nil, ErrForbidden
	}
	if err := validateOptionalID(courseID); err != nil {
		return nil, err
	}
	limit, err := sessionLimit(limit)
	if err != nil {
		return nil, err
	}
	return s.repo.SessionMix(ctx, user.ID, courseID, limit)
}

// ReviewCard stores a student's 1 to 5 rating of a card they may study.
func (s *FlashcardService) ReviewCard(ctx context.Context, user *models.User, cardID string, rating int) error {
	if user.Role != models.RoleStudent {
		return ErrForbidden
	}
	if err := validateRating(rating); err != nil {
		return err
	}
	card, _, _, tier, err := s.loadCard(ctx, user, cardID)
	if err != nil {
		return err
	}
	if !models.CanAccess(tier, card.Tiers) {
		return ErrFlashcardNotFound
	}
	return s.repo.UpsertReview(ctx, user.ID, card.ID, rating)
}
