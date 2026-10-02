package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	maxQuizBodyBytes    = 4 << 20  // 4 MB: room for 200 long questions with 10 options each
	maxAttemptBodyBytes = 64 << 10 // 64 KB: 200 question/option id pairs
)

type CreateQuizRequest struct {
	Title        string                  `json:"title"`
	Description  string                  `json:"description"`
	TimeLimitSec *int                    `json:"timeLimitSec"` // omit for an untimed quiz
	PassPercent  *int                    `json:"passPercent"`  // defaults to 70
	IsFree       bool                    `json:"isFree"`
	Status       string                  `json:"status"` // draft or published, defaults to published
	Questions    []CreateQuestionRequest `json:"questions"`
}

type CreateQuestionRequest struct {
	QuestionText string                `json:"questionText"`
	Explanation  string                `json:"explanation"` // shown to the student after submitting
	Options      []CreateOptionRequest `json:"options"`
}

type CreateOptionRequest struct {
	OptionText string `json:"optionText"`
	IsCorrect  bool   `json:"isCorrect"`
}

type UpdateQuizStatusRequest struct {
	Status string `json:"status"` // draft or published
}

type SubmitAttemptRequest struct {
	Answers []SubmitAnswerRequest `json:"answers"`
}

type SubmitAnswerRequest struct {
	QuestionID string `json:"questionId"`
	OptionID   string `json:"optionId"`
}

type QuizHandler struct {
	quizService *service.QuizService
}

func NewQuizHandler(quizService *service.QuizService) *QuizHandler {
	return &QuizHandler{
		quizService: quizService,
	}
}

// CreateQuiz godoc
//
//	@Summary		Create a quiz on a lesson
//	@Description	Instructor creates a quiz with all its questions and options in one request, on a lesson of a course they teach. Each question needs 2 to 10 options with exactly one correct. Questions and options keep the order they are sent in. Published by default.
//	@Tags			quizzes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Lesson ID"
//	@Param			request	body		CreateQuizRequest						true	"Quiz with questions and options"
//	@Success		201		{object}	utils.JSONResponse{data=models.Quiz}	"Quiz created"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor"
//	@Failure		404		{object}	utils.JSONResponse						"Lesson not found, or not in a course this user teaches"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/lessons/{id}/quizzes [post]
func (h *QuizHandler) CreateQuiz(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQuizBodyBytes)
	var req CreateQuizRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	in := service.CreateQuizInput{
		Title:        req.Title,
		Description:  req.Description,
		TimeLimitSec: req.TimeLimitSec,
		PassPercent:  req.PassPercent,
		IsFree:       req.IsFree,
		Status:       req.Status,
	}
	for _, q := range req.Questions {
		question := service.CreateQuestionInput{
			QuestionText: q.QuestionText,
			Explanation:  q.Explanation,
		}
		for _, o := range q.Options {
			question.Options = append(question.Options, service.CreateOptionInput{
				OptionText: o.OptionText,
				IsCorrect:  o.IsCorrect,
			})
		}
		in.Questions = append(in.Questions, question)
	}

	quiz, err := h.quizService.CreateQuiz(r.Context(), user, r.PathValue("id"), in)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, quiz)
}

// UpdateQuizStatus godoc
//
//	@Summary		Change quiz status
//	@Description	Instructor of the course publishes a quiz or moves it back to draft. Students only see published quizzes.
//	@Tags			quizzes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Quiz ID"
//	@Param			request	body		UpdateQuizStatusRequest					true	"New status"
//	@Success		200		{object}	utils.JSONResponse{data=models.Quiz}	"Quiz updated"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid status"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor"
//	@Failure		404		{object}	utils.JSONResponse						"Quiz not found, or not in a course this user teaches"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/quizzes/{id}/status [patch]
func (h *QuizHandler) UpdateQuizStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAttemptBodyBytes)
	var req UpdateQuizStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	quiz, err := h.quizService.UpdateQuizStatus(r.Context(), user, r.PathValue("id"), req.Status)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, quiz)
}

// ListQuizzes godoc
//
//	@Summary		List a lesson's quizzes
//	@Description	Returns the quizzes of a lesson without their questions. Only the course instructor and enrolled students can see them. Students see published quizzes only.
//	@Tags			quizzes
//	@Produce		json
//	@Param			id	path		string									true	"Lesson ID"
//	@Success		200	{object}	utils.JSONResponse{data=[]models.Quiz}	"Quizzes"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse						"Not enrolled in this course"
//	@Failure		404	{object}	utils.JSONResponse						"Lesson not found"
//	@Failure		500	{object}	utils.JSONResponse						"Internal server error"
//	@Router			/lessons/{id}/quizzes [get]
func (h *QuizHandler) ListQuizzes(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	quizzes, err := h.quizService.ListQuizzes(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, quizzes)
}

// GetQuiz godoc
//
//	@Summary		Get a quiz with its questions
//	@Description	Returns the quiz with its questions and options in order. The instructor also gets isCorrect and explanations; students never do before submitting.
//	@Tags			quizzes
//	@Produce		json
//	@Param			id	path		string									true	"Quiz ID"
//	@Success		200	{object}	utils.JSONResponse{data=models.Quiz}	"Quiz with questions"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		404	{object}	utils.JSONResponse						"Quiz not found, or not visible to this user"
//	@Failure		500	{object}	utils.JSONResponse						"Internal server error"
//	@Router			/quizzes/{id} [get]
func (h *QuizHandler) GetQuiz(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	quiz, err := h.quizService.GetQuiz(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, quiz)
}

// SubmitAttempt godoc
//
//	@Summary		Submit answers to a quiz
//	@Description	Student sends all answers in one request and gets the graded result back, with the correct option and explanation for every question. Questions left out count as skipped (wrong). Every submission is saved as a new attempt.
//	@Tags			quizzes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Quiz ID"
//	@Param			request	body		SubmitAttemptRequest							true	"Selected option per question"
//	@Success		201		{object}	utils.JSONResponse{data=models.QuizAttempt}		"Graded attempt"
//	@Failure		400		{object}	utils.JSONResponse								"Malformed payload, or answer not part of this quiz"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not a student"
//	@Failure		404		{object}	utils.JSONResponse								"Quiz not found, or not visible to this user"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/quizzes/{id}/attempts [post]
func (h *QuizHandler) SubmitAttempt(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAttemptBodyBytes)
	var req SubmitAttemptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	answers := make([]service.SubmitAnswerInput, 0, len(req.Answers))
	for _, a := range req.Answers {
		answers = append(answers, service.SubmitAnswerInput{QuestionID: a.QuestionID, OptionID: a.OptionID})
	}

	attempt, err := h.quizService.SubmitAttempt(r.Context(), user, r.PathValue("id"), answers)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, attempt)
}

// ListAttempts godoc
//
//	@Summary		List my attempts at a quiz
//	@Description	Returns the logged-in student's past attempts at a quiz, newest first. Each attempt has its result and, per question, the selected option, the correct option and the explanation.
//	@Tags			quizzes
//	@Produce		json
//	@Param			id	path		string										true	"Quiz ID"
//	@Success		200	{object}	utils.JSONResponse{data=[]models.QuizAttempt}	"Attempts"
//	@Failure		401	{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse							"Not a student"
//	@Failure		404	{object}	utils.JSONResponse							"Quiz not found, or not visible to this user"
//	@Failure		500	{object}	utils.JSONResponse							"Internal server error"
//	@Router			/quizzes/{id}/attempts [get]
func (h *QuizHandler) ListAttempts(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	attempts, err := h.quizService.ListAttempts(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, attempts)
}
