package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const maxFlashcardBodyBytes = 64 << 10 // 64 KB: a card is at most 2500 characters

type CreateDeckRequest struct {
	LessonID *string `json:"lessonId"` // omit for the course-wide deck
	Status   string  `json:"status"`   // draft or published, defaults to draft
}

type UpdateDeckStatusRequest struct {
	Status string `json:"status"` // draft or published
}

// SetDeckTiersRequest gives all of a deck's cards the same tiers: free, plus, premium (ultra is always included).
type SetDeckTiersRequest struct {
	Tiers []string `json:"tiers"`
}

// SetDeckFreeRequest is the deprecated form of SetDeckTiersRequest.
type SetDeckFreeRequest struct {
	IsFree bool `json:"isFree"`
}

type CreateFlashcardRequest struct {
	Front  string   `json:"front"`
	Back   string   `json:"back"`
	Tiers  []string `json:"tiers"`  // free, plus, premium; defaults to plus and premium; ultra is always included
	IsFree bool     `json:"isFree"` // deprecated: use tiers
}

// UpdateFlashcardRequest changes only the fields that are sent.
type UpdateFlashcardRequest struct {
	Front  *string  `json:"front"`
	Back   *string  `json:"back"`
	Tiers  []string `json:"tiers"`
	IsFree *bool    `json:"isFree"` // deprecated: use tiers
}

type ReviewFlashcardRequest struct {
	Rating int `json:"rating"` // 1 (no idea) to 5 (perfect)
}

type FlashcardHandler struct {
	flashcardService *service.FlashcardService
}

func NewFlashcardHandler(flashcardService *service.FlashcardService) *FlashcardHandler {
	return &FlashcardHandler{flashcardService: flashcardService}
}

// decodeFlashcardBody reads a small JSON body into v, answering 400 itself when it is malformed.
func decodeFlashcardBody(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFlashcardBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return false
	}
	return true
}

// ListCourseDecks godoc
//
//	@Summary		List the flashcard decks of a course
//	@Description	One deck per lesson plus the course-wide deck (lessonId null), with card counts, free card counts and the user's own review progress. The course owner also sees drafts; everyone else only published decks of published lessons.
//	@Tags			flashcards
//	@Produce		json
//	@Param			id	path		string											true	"Course ID"
//	@Success		200	{object}	utils.JSONResponse{data=[]models.FlashcardDeck}	"Decks"
//	@Failure		401	{object}	utils.JSONResponse								"Not logged in"
//	@Failure		404	{object}	utils.JSONResponse								"Course not found, or not visible to this user"
//	@Failure		500	{object}	utils.JSONResponse								"Internal server error"
//	@Router			/courses/{id}/flashcard-decks [get]
func (h *FlashcardHandler) ListCourseDecks(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	decks, err := h.flashcardService.ListCourseDecks(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, decks)
}

// CreateDeck godoc
//
//	@Summary		Create a flashcard deck
//	@Description	Course owner creates the deck of a lesson, or the course-wide deck when lessonId is omitted. A draft by default. One deck per lesson: 409 if it exists.
//	@Tags			flashcards
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Course ID"
//	@Param			request	body		CreateDeckRequest						true	"Lesson and status"
//	@Success		201		{object}	utils.JSONResponse{data=models.FlashcardDeck}	"Deck created"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not the course owner"
//	@Failure		404		{object}	utils.JSONResponse						"Course or lesson not found"
//	@Failure		409		{object}	utils.JSONResponse						"The lesson already has a deck"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/courses/{id}/flashcard-decks [post]
func (h *FlashcardHandler) CreateDeck(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	var req CreateDeckRequest
	if !decodeFlashcardBody(w, r, &req) {
		return
	}
	deck, err := h.flashcardService.CreateDeck(r.Context(), user, r.PathValue("id"),
		service.CreateDeckInput{LessonID: req.LessonID, Status: req.Status})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, deck)
}

// UpdateDeckStatus godoc
//
//	@Summary		Publish a flashcard deck or turn it back into a draft
//	@Description	Course owner only. Students only see published decks.
//	@Tags			flashcards
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Deck ID"
//	@Param			request	body		UpdateDeckStatusRequest							true	"New status"
//	@Success		200		{object}	utils.JSONResponse{data=models.FlashcardDeck}	"Deck updated"
//	@Failure		400		{object}	utils.JSONResponse								"Malformed payload or invalid status"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not the course owner"
//	@Failure		404		{object}	utils.JSONResponse								"Deck not found"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/flashcard-decks/{id} [patch]
func (h *FlashcardHandler) UpdateDeckStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	var req UpdateDeckStatusRequest
	if !decodeFlashcardBody(w, r, &req) {
		return
	}
	deck, err := h.flashcardService.UpdateDeckStatus(r.Context(), user, r.PathValue("id"), req.Status)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, deck)
}

// SetDeckTiers godoc
//
//	@Summary		Set the tiers of every card of a deck
//	@Description	Course owner only. Gives all the deck's cards the same tiers at once (free, plus, premium; ultra is always included). Cards tagged free can be studied by users who are not enrolled.
//	@Tags			flashcards
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Deck ID"
//	@Param			request	body		SetDeckTiersRequest								true	"Tiers"
//	@Success		200		{object}	utils.JSONResponse{data=models.FlashcardDeck}	"Deck updated"
//	@Failure		400		{object}	utils.JSONResponse								"Malformed payload or unknown tier"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not the course owner"
//	@Failure		404		{object}	utils.JSONResponse								"Deck not found"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/flashcard-decks/{id}/tiers [put]
func (h *FlashcardHandler) SetDeckTiers(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	var req SetDeckTiersRequest
	if !decodeFlashcardBody(w, r, &req) {
		return
	}
	deck, err := h.flashcardService.SetDeckTiers(r.Context(), user, r.PathValue("id"), req.Tiers)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, deck)
}

// SetDeckFree godoc
//
//	@Summary		Make every card of a deck free or paid
//	@Description	Deprecated, use PUT /flashcard-decks/{id}/tiers. Course owner only. true gives all the deck's cards the free tier, false the default paid tiers.
//	@Deprecated
//	@Tags			flashcards
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Deck ID"
//	@Param			request	body		SetDeckFreeRequest								true	"Free or paid"
//	@Success		200		{object}	utils.JSONResponse{data=models.FlashcardDeck}	"Deck updated"
//	@Failure		400		{object}	utils.JSONResponse								"Malformed payload"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not the course owner"
//	@Failure		404		{object}	utils.JSONResponse								"Deck not found"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/flashcard-decks/{id}/free [put]
func (h *FlashcardHandler) SetDeckFree(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	var req SetDeckFreeRequest
	if !decodeFlashcardBody(w, r, &req) {
		return
	}
	deck, err := h.flashcardService.SetDeckFree(r.Context(), user, r.PathValue("id"), req.IsFree)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, deck)
}

// DeleteDeck godoc
//
//	@Summary		Delete a flashcard deck
//	@Description	Course owner only. Soft-deletes the deck and all its cards.
//	@Tags			flashcards
//	@Produce		json
//	@Param			id	path		string							true	"Deck ID"
//	@Success		200	{object}	utils.JSONResponse{data=string}	"Deck deleted"
//	@Failure		401	{object}	utils.JSONResponse				"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse				"Not the course owner"
//	@Failure		404	{object}	utils.JSONResponse				"Deck not found"
//	@Failure		500	{object}	utils.JSONResponse				"Internal server error"
//	@Router			/flashcard-decks/{id} [delete]
func (h *FlashcardHandler) DeleteDeck(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	if err := h.flashcardService.DeleteDeck(r.Context(), user, r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "flashcard deck deleted")
}

// ListDeckCards godoc
//
//	@Summary		List the cards of a deck
//	@Description	Cards in order, each with the user's own rating. Cards the user's tier cannot study come back with locked true and front and back blank.
//	@Tags			flashcards
//	@Produce		json
//	@Param			id	path		string										true	"Deck ID"
//	@Success		200	{object}	utils.JSONResponse{data=[]models.Flashcard}	"Cards"
//	@Failure		401	{object}	utils.JSONResponse							"Not logged in"
//	@Failure		404	{object}	utils.JSONResponse							"Deck not found, or not visible to this user"
//	@Failure		500	{object}	utils.JSONResponse							"Internal server error"
//	@Router			/flashcard-decks/{id}/cards [get]
func (h *FlashcardHandler) ListDeckCards(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	cards, err := h.flashcardService.ListDeckCards(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, cards)
}

// CreateCard godoc
//
//	@Summary		Add a flashcard to a deck
//	@Description	Course owner only. The card goes to the end of the deck. front is at most 500 characters, back at most 2000.
//	@Tags			flashcards
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Deck ID"
//	@Param			request	body		CreateFlashcardRequest					true	"Card"
//	@Success		201		{object}	utils.JSONResponse{data=models.Flashcard}	"Card created"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not the course owner"
//	@Failure		404		{object}	utils.JSONResponse						"Deck not found"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/flashcard-decks/{id}/cards [post]
func (h *FlashcardHandler) CreateCard(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	var req CreateFlashcardRequest
	if !decodeFlashcardBody(w, r, &req) {
		return
	}
	card, err := h.flashcardService.CreateCard(r.Context(), user, r.PathValue("id"),
		service.CreateCardInput{Front: req.Front, Back: req.Back, Tiers: req.Tiers, IsFree: req.IsFree})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, card)
}

// UpdateCard godoc
//
//	@Summary		Edit a flashcard
//	@Description	Course owner only. Changes only the fields that are sent.
//	@Tags			flashcards
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string										true	"Card ID"
//	@Param			request	body		UpdateFlashcardRequest						true	"Fields to change"
//	@Success		200		{object}	utils.JSONResponse{data=models.Flashcard}	"Card updated"
//	@Failure		400		{object}	utils.JSONResponse							"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse							"Not the course owner"
//	@Failure		404		{object}	utils.JSONResponse							"Card not found"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/flashcards/{id} [patch]
func (h *FlashcardHandler) UpdateCard(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	var req UpdateFlashcardRequest
	if !decodeFlashcardBody(w, r, &req) {
		return
	}
	card, err := h.flashcardService.UpdateCard(r.Context(), user, r.PathValue("id"),
		service.UpdateCardInput{Front: req.Front, Back: req.Back, Tiers: req.Tiers, IsFree: req.IsFree})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, card)
}

// DeleteCard godoc
//
//	@Summary		Delete a flashcard
//	@Description	Course owner only.
//	@Tags			flashcards
//	@Produce		json
//	@Param			id	path		string							true	"Card ID"
//	@Success		200	{object}	utils.JSONResponse{data=string}	"Card deleted"
//	@Failure		401	{object}	utils.JSONResponse				"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse				"Not the course owner"
//	@Failure		404	{object}	utils.JSONResponse				"Card not found"
//	@Failure		500	{object}	utils.JSONResponse				"Internal server error"
//	@Router			/flashcards/{id} [delete]
func (h *FlashcardHandler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	if err := h.flashcardService.DeleteCard(r.Context(), user, r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "flashcard deleted")
}

// DeckSession godoc
//
//	@Summary		Study session for a deck
//	@Description	Distinct cards of the deck in random order; cards rated low by the user come up more often. Only cards the user's tier can study are included.
//	@Tags			flashcards
//	@Produce		json
//	@Param			id		path		string										true	"Deck ID"
//	@Param			limit	query		int											false	"Number of cards (default 20, max 100)"
//	@Success		200		{object}	utils.JSONResponse{data=[]models.Flashcard}	"Cards"
//	@Failure		400		{object}	utils.JSONResponse							"Invalid limit"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		404		{object}	utils.JSONResponse							"Deck not found, or not visible to this user"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/flashcard-decks/{id}/session [get]
func (h *FlashcardHandler) DeckSession(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	limit, err := intQuery(r, "limit", 0)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be a number")
		return
	}
	cards, err := h.flashcardService.DeckSession(r.Context(), user, r.PathValue("id"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, cards)
}

// ListMyFlashcards godoc
//
//	@Summary		My flashcard decks
//	@Description	Students only. The published decks with cards in the courses the student is enrolled in, with their review progress, plus recent: the deck of the lesson they watched or studied last (null if none).
//	@Tags			me
//	@Produce		json
//	@Param			courseId	query		string											false	"Only this course"
//	@Success		200			{object}	utils.JSONResponse{data=service.MyFlashcards}	"Decks"
//	@Failure		400			{object}	utils.JSONResponse								"Invalid courseId"
//	@Failure		401			{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403			{object}	utils.JSONResponse								"Not a student"
//	@Failure		500			{object}	utils.JSONResponse								"Internal server error"
//	@Router			/me/flashcards [get]
func (h *FlashcardHandler) ListMyFlashcards(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	res, err := h.flashcardService.MyFlashcards(r.Context(), user, r.URL.Query().Get("courseId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, res)
}

// MixSession godoc
//
//	@Summary		Random mix of flashcards
//	@Description	Students only. Distinct cards from the published decks of the student's courses in random order; cards rated low come up more often.
//	@Tags			me
//	@Produce		json
//	@Param			courseId	query		string										false	"Only this course"
//	@Param			limit		query		int											false	"Number of cards (default 20, max 100)"
//	@Success		200			{object}	utils.JSONResponse{data=[]models.Flashcard}	"Cards"
//	@Failure		400			{object}	utils.JSONResponse							"Invalid courseId or limit"
//	@Failure		401			{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403			{object}	utils.JSONResponse							"Not a student"
//	@Failure		500			{object}	utils.JSONResponse							"Internal server error"
//	@Router			/me/flashcards/session [get]
func (h *FlashcardHandler) MixSession(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	limit, err := intQuery(r, "limit", 0)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be a number")
		return
	}
	cards, err := h.flashcardService.MixSession(r.Context(), user, r.URL.Query().Get("courseId"), limit)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, cards)
}

// ReviewCard godoc
//
//	@Summary		Rate a flashcard
//	@Description	Students only. Stores the student's latest self-rating, 1 (no idea) to 5 (perfect). Low-rated cards come up more often in sessions.
//	@Tags			flashcards
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string							true	"Card ID"
//	@Param			request	body		ReviewFlashcardRequest			true	"Rating"
//	@Success		200		{object}	utils.JSONResponse{data=string}	"Rating saved"
//	@Failure		400		{object}	utils.JSONResponse				"Malformed payload or rating outside 1 to 5"
//	@Failure		401		{object}	utils.JSONResponse				"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse				"Not a student"
//	@Failure		404		{object}	utils.JSONResponse				"Card not found, or locked for this user"
//	@Failure		500		{object}	utils.JSONResponse				"Internal server error"
//	@Router			/flashcards/{id}/review [put]
func (h *FlashcardHandler) ReviewCard(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	var req ReviewFlashcardRequest
	if !decodeFlashcardBody(w, r, &req) {
		return
	}
	if err := h.flashcardService.ReviewCard(r.Context(), user, r.PathValue("id"), req.Rating); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "rating saved")
}
