package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	maxQuizBodyBytes    = 4 << 20  // 4 MB: room for 200 long questions with 10 options each
	maxAttemptBodyBytes = 64 << 10 // 64 KB: 200 question/option id pairs
)

type CreateQuizRequest struct {
	Title        string                  `json:"title"`
	Description  string                  `json:"description"`
	Type         string                  `json:"type"`         // mock_test or practice, defaults to mock_test
	TimeLimitSec *int                    `json:"timeLimitSec"` // mock_test only; omit for an untimed quiz
	PassPercent  *int                    `json:"passPercent"`  // mock_test only; defaults to 70
	Tiers        []string                `json:"tiers"`        // free, plus, premium; defaults to plus and premium; ultra is always included
	IsFree       bool                    `json:"isFree"`       // deprecated: use tiers
	Status       string                  `json:"status"`       // draft or published, defaults to published
	Questions    []CreateQuestionRequest `json:"questions"`
}

type CreateQuestionRequest struct {
	QuestionText string                `json:"questionText"`
	Explanation  string                `json:"explanation"` // shown to the student after checking (practice) or submitting (mock test)
	Options      []CreateOptionRequest `json:"options"`
}

type CreateOptionRequest struct {
	OptionText string `json:"optionText"`
	IsCorrect  bool   `json:"isCorrect"`
}

// UpdateQuizRequest changes only the fields that are sent. timeLimitSec 0 makes the quiz untimed.
// questions, when sent, replace all existing questions; refused with 409 once the quiz has attempts.
type UpdateQuizRequest struct {
	Title        *string                  `json:"title"`
	Description  *string                  `json:"description"`
	Type         *string                  `json:"type"`
	TimeLimitSec *int                     `json:"timeLimitSec"`
	PassPercent  *int                     `json:"passPercent"`
	Tiers        []string                 `json:"tiers"`
	IsFree       *bool                    `json:"isFree"` // deprecated: use tiers
	Status       *string                  `json:"status"`
	Questions    *[]CreateQuestionRequest `json:"questions"`
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
//	@Description	Course owner (instructor or admin) creates a quiz with all its questions and options in one request, on a lesson of their course. Each question needs 2 to 10 options with exactly one correct. Questions and options keep the order they are sent in. Published by default. type is mock_test (default: optional timeLimitSec, passPercent defaults to 70, graded with marks) or practice (untimed, no marks; timeLimitSec and passPercent must be omitted).
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
		Type:         req.Type,
		TimeLimitSec: req.TimeLimitSec,
		PassPercent:  req.PassPercent,
		Tiers:        req.Tiers,
		IsFree:       req.IsFree,
		Status:       req.Status,
		Questions:    questionInputs(req.Questions),
	}

	quiz, err := h.quizService.CreateQuiz(r.Context(), user, r.PathValue("id"), in)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, quiz)
}

// questionInputs converts request questions into service input, keeping their order.
func questionInputs(req []CreateQuestionRequest) []service.CreateQuestionInput {
	questions := make([]service.CreateQuestionInput, 0, len(req))
	for _, q := range req {
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
		questions = append(questions, question)
	}
	return questions
}

// UpdateQuiz godoc
//
//	@Summary		Edit a quiz
//	@Description	Course owner (instructor or admin) changes only the fields that are sent. timeLimitSec 0 makes the quiz untimed. Sending questions replaces all of them, which is refused once any student has attempted the quiz. Changing type resets timeLimitSec and passPercent to the new type's defaults and, like replacing questions, is refused once any student has attempted the quiz. Returns the quiz with its questions and answers.
//	@Tags			quizzes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Quiz ID"
//	@Param			request	body		UpdateQuizRequest						true	"Fields to change"
//	@Success		200		{object}	utils.JSONResponse{data=models.Quiz}	"Quiz updated"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor or admin"
//	@Failure		404		{object}	utils.JSONResponse						"Quiz not found, or not in a course this user owns"
//	@Failure		409		{object}	utils.JSONResponse						"Questions or type cannot change: the quiz already has attempts"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/quizzes/{id} [patch]
func (h *QuizHandler) UpdateQuiz(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQuizBodyBytes)
	var req UpdateQuizRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	in := service.UpdateQuizInput{
		Title:        req.Title,
		Description:  req.Description,
		Type:         req.Type,
		TimeLimitSec: req.TimeLimitSec,
		PassPercent:  req.PassPercent,
		Tiers:        req.Tiers,
		IsFree:       req.IsFree,
		Status:       req.Status,
	}
	if req.Questions != nil {
		questions := questionInputs(*req.Questions)
		in.Questions = &questions
	}

	quiz, err := h.quizService.UpdateQuiz(r.Context(), user, r.PathValue("id"), in)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, quiz)
}

// DeleteQuiz godoc
//
//	@Summary		Delete a quiz
//	@Description	Course owner (instructor or admin) deletes a quiz. It disappears for everyone, but students' past attempts and scores are kept in the database.
//	@Tags			quizzes
//	@Produce		json
//	@Param			id	path		string							true	"Quiz ID"
//	@Success		200	{object}	utils.JSONResponse{data=string}	"Quiz deleted"
//	@Failure		401	{object}	utils.JSONResponse				"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse				"Not an instructor or admin"
//	@Failure		404	{object}	utils.JSONResponse				"Quiz not found, or not in a course this user owns"
//	@Failure		500	{object}	utils.JSONResponse				"Internal server error"
//	@Router			/quizzes/{id} [delete]
func (h *QuizHandler) DeleteQuiz(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	if err := h.quizService.DeleteQuiz(r.Context(), user, r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "quiz deleted")
}

// UpdateQuizStatus godoc
//
//	@Summary		Change quiz status
//	@Description	Course owner (instructor or admin) publishes a quiz or moves it back to draft. Students only see published quizzes.
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
//	@Description	Returns the quizzes of a lesson without their questions. The course instructor and enrolled students can see them; students see published quizzes only. Users not enrolled, or enrolled on a lower tier, see them too, with locked set on those their tier cannot open (a free lesson does not unlock its quizzes). Filter with ?type=mock_test or ?type=practice.
//	@Tags			quizzes
//	@Produce		json
//	@Param			id	path		string									true	"Lesson ID"
//	@Param			type	query		string									false	"mock_test or practice; omit for both"
//	@Success		200	{object}	utils.JSONResponse{data=[]models.Quiz}	"Quizzes"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		400	{object}	utils.JSONResponse						"Invalid type filter"
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

	quizzes, err := h.quizService.ListQuizzes(r.Context(), user, r.PathValue("id"), r.URL.Query().Get("type"))
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
//	@Summary		Save a finished practice session
//	@Description	Practice sets only. At the end of a session the student sends every question they answered in one request and gets them graded back, with the correct option and explanation for each, to show on the review page. Questions left out are not graded; at least one answer is required. score, total and passed are null: practice has no marks. Every submission is saved as a new attempt. Mock tests are refused with 400; submit them with POST /quizzes/{id}/submit.
//	@Tags			quizzes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Quiz ID"
//	@Param			request	body		SubmitAttemptRequest							true	"Selected option per question"
//	@Success		201		{object}	utils.JSONResponse{data=models.QuizAttempt}		"Graded attempt"
//	@Failure		400		{object}	utils.JSONResponse								"Malformed payload, answer not part of this quiz, or a mock test"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not a student"
//	@Failure		404		{object}	utils.JSONResponse								"Quiz not found, or not visible to this user"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/quizzes/{id}/attempts [post]
func (h *QuizHandler) SubmitAttempt(w http.ResponseWriter, r *http.Request) {
	user, answers, ok := decodeAnswers(w, r)
	if !ok {
		return
	}
	attempt, err := h.quizService.SubmitAttempt(r.Context(), user, r.PathValue("id"), answers)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, attempt)
}

// CheckAnswer godoc
//
//	@Summary		Check one practice answer
//	@Description	Practice sets only. The student sends the option they picked for one question and immediately learns whether it is right, which option is correct, and why. Nothing is saved; the whole session is saved at the end with POST /quizzes/{id}/attempts. Mock tests are refused with 400.
//	@Tags			quizzes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Quiz ID"
//	@Param			request	body		SubmitAnswerRequest								true	"Selected option"
//	@Success		200		{object}	utils.JSONResponse{data=models.AttemptAnswer}	"Graded answer"
//	@Failure		400		{object}	utils.JSONResponse								"Malformed payload, answer not part of this quiz, or a mock test"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not a student"
//	@Failure		404		{object}	utils.JSONResponse								"Quiz not found, or not visible to this user"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/quizzes/{id}/check [post]
func (h *QuizHandler) CheckAnswer(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAttemptBodyBytes)
	var req SubmitAnswerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	answer, err := h.quizService.CheckAnswer(r.Context(), user, r.PathValue("id"), service.SubmitAnswerInput{QuestionID: req.QuestionID, OptionID: req.OptionID})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, answer)
}

// SubmitMockTest godoc
//
//	@Summary		Submit a mock test
//	@Description	Mock tests only. The student sends all answers in one request; questions left out count as skipped (wrong). The attempt is saved and the marks come back with answers: every question graded, with the selected option (null if skipped), the correct option and the explanation. Practice sets are refused with 400.
//	@Tags			quizzes
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string										true	"Quiz ID"
//	@Param			request	body		SubmitAttemptRequest						true	"Selected option per question"
//	@Success		201		{object}	utils.JSONResponse{data=models.MockResult}	"Marks and graded answers"
//	@Failure		400		{object}	utils.JSONResponse							"Malformed payload, answer not part of this quiz, or a practice set"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse							"Not a student"
//	@Failure		404		{object}	utils.JSONResponse							"Quiz not found, or not visible to this user"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/quizzes/{id}/submit [post]
func (h *QuizHandler) SubmitMockTest(w http.ResponseWriter, r *http.Request) {
	user, answers, ok := decodeAnswers(w, r)
	if !ok {
		return
	}
	result, err := h.quizService.SubmitMockTest(r.Context(), user, r.PathValue("id"), answers)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, result)
}

// decodeAnswers reads the logged-in user and a SubmitAttemptRequest. On failure it has already written
// the response and returns ok false.
func decodeAnswers(w http.ResponseWriter, r *http.Request) (*models.User, []service.SubmitAnswerInput, bool) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return nil, nil, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAttemptBodyBytes)
	var req SubmitAttemptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return nil, nil, false
	}

	answers := make([]service.SubmitAnswerInput, 0, len(req.Answers))
	for _, a := range req.Answers {
		answers = append(answers, service.SubmitAnswerInput{QuestionID: a.QuestionID, OptionID: a.OptionID})
	}
	return user, answers, true
}

// ListAttempts godoc
//
//	@Summary		List my attempts at a quiz
//	@Description	Returns the logged-in student's past attempts at a quiz, newest first. A practice session has, per question, the selected option, the correct option and the explanation. A mock test attempt also has its marks, and its answers cover every question (selectedOptionId null if skipped).
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
