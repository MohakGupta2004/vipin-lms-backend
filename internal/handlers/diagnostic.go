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
	maxDiagnosticBodyBytes = 64 << 10 // 64 KB
	defaultDiagnosticLimit = 50
)

// SaveDiagnosticResultRequest carries only the answers: the server scores them against an answer key
// the browser never sees. Older clients also sent correct, total and elements; those are ignored.
type SaveDiagnosticResultRequest struct {
	// TestSlug is the test these answers are for. Empty means the CIRE skill diagnostic (cire-advanced).
	TestSlug string                    `json:"testSlug"`
	Answers  []models.DiagnosticAnswer `json:"answers"`
}

type DiagnosticStatusResponse struct {
	Taken bool `json:"taken"`
}

type DiagnosticHandler struct {
	diagnosticService *service.DiagnosticService
}

func NewDiagnosticHandler(diagnosticService *service.DiagnosticService) *DiagnosticHandler {
	return &DiagnosticHandler{diagnosticService: diagnosticService}
}

// ListTests godoc
//
//	@Summary		List diagnostic tests
//	@Description	Any logged-in user. Every active diagnostic test in display order, each with whether the caller already took it. Never includes a score.
//	@Tags			diagnostic
//	@Produce		json
//	@Success		200	{object}	utils.JSONResponse{data=[]models.DiagnosticTest}	"Tests"
//	@Failure		401	{object}	utils.JSONResponse								"Not logged in"
//	@Failure		500	{object}	utils.JSONResponse								"Internal server error"
//	@Router			/diagnostic/tests [get]
func (h *DiagnosticHandler) ListTests(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	tests, err := h.diagnosticService.ListTests(r.Context(), user)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, tests)
}

// GetTest godoc
//
//	@Summary		Get one diagnostic test
//	@Description	Any logged-in user. Same as a list item. The frontend uses it to show the test's intro and to block a second attempt.
//	@Tags			diagnostic
//	@Produce		json
//	@Param			slug	path		string	true	"Diagnostic test slug, e.g. cire-advanced"
//	@Success		200		{object}	utils.JSONResponse{data=models.DiagnosticTest}	"Test"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		404		{object}	utils.JSONResponse							"No active test with this slug"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/diagnostic/tests/{slug} [get]
func (h *DiagnosticHandler) GetTest(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	test, err := h.diagnosticService.GetTest(r.Context(), user, r.PathValue("slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, test)
}

// GetQuestions godoc
//
//	@Summary		Get a diagnostic test's questions
//	@Description	Any logged-in user. The questions and options to answer, in order, without chapters or correct answers: attempts are scored on the server. 409 once the caller has taken the test.
//	@Tags			diagnostic
//	@Produce		json
//	@Param			slug	path		string	true	"Diagnostic test slug, e.g. cire-advanced"
//	@Success		200		{object}	utils.JSONResponse{data=[]models.DiagnosticQuestion}	"Questions"
//	@Failure		401		{object}	utils.JSONResponse									"Not logged in"
//	@Failure		404		{object}	utils.JSONResponse									"No active test with this slug"
//	@Failure		409		{object}	utils.JSONResponse									"Diagnostic test already taken"
//	@Failure		500		{object}	utils.JSONResponse									"Internal server error"
//	@Router			/diagnostic/tests/{slug}/questions [get]
func (h *DiagnosticHandler) GetQuestions(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	questions, err := h.diagnosticService.GetQuestions(r.Context(), user, r.PathValue("slug"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, questions)
}

// SaveResult godoc
//
//	@Summary		Save a diagnostic result
//	@Description	Any logged-in user. Scores up to 100 answers of a completed diagnostic test on the server and stores the score, the per-element results and the answers. testSlug names the test (default cire-advanced). Questions left out count as skipped. Each email can submit each test only once. The response has no score: only admins can see results.
//	@Tags			diagnostic
//	@Accept			json
//	@Produce		json
//	@Param			request	body		SaveDiagnosticResultRequest					true	"Diagnostic result"
//	@Success		201		{object}	utils.JSONResponse{data=models.DiagnosticResult}	"Saved"
//	@Failure		400		{object}	utils.JSONResponse							"Malformed payload or invalid result"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		404		{object}	utils.JSONResponse							"No active test with this slug"
//	@Failure		409		{object}	utils.JSONResponse							"Diagnostic test already taken"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/diagnostic/results [post]
func (h *DiagnosticHandler) SaveResult(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDiagnosticBodyBytes)
	var req SaveDiagnosticResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	result, err := h.diagnosticService.SaveResult(r.Context(), user, service.SaveDiagnosticResultInput{
		TestSlug: req.TestSlug,
		Answers:  req.Answers,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, result)
}

// Status godoc
//
//	@Summary		Did I already take a diagnostic test
//	@Description	Any logged-in user. The frontend uses this to block a second attempt. Prefer GET /diagnostic/tests/{slug}, which also returns the test.
//	@Tags			diagnostic
//	@Produce		json
//	@Param			test	query		string	false	"Diagnostic test slug (default cire-advanced)"
//	@Success		200		{object}	utils.JSONResponse{data=DiagnosticStatusResponse}	"Status"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		404		{object}	utils.JSONResponse								"No active test with this slug"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/diagnostic/status [get]
func (h *DiagnosticHandler) Status(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	test, err := h.diagnosticService.GetTest(r.Context(), user, r.URL.Query().Get("test"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, DiagnosticStatusResponse{Taken: test.Taken})
}

// ListResults godoc
//
//	@Summary		List diagnostic results
//	@Description	Admin only. Newest first. q matches first name, last name or email.
//	@Tags			admin-diagnostic
//	@Produce		json
//	@Param			q		query		string	false	"Case-insensitive search on name or email"
//	@Param			limit	query		int		false	"Max results (default 50, max 200)"
//	@Param			offset	query		int		false	"Number to skip (default 0)"
//	@Success		200		{object}	utils.JSONResponse{data=models.AdminDiagnosticResultsPage}	"Results"
//	@Failure		400		{object}	utils.JSONResponse								"Invalid limit or offset"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/admin/diagnostic/results [get]
func (h *DiagnosticHandler) ListResults(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", defaultDiagnosticLimit)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be a number")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be a number")
		return
	}

	page, err := h.diagnosticService.ListResults(r.Context(), user, service.DiagnosticResultsFilter{
		Q:      r.URL.Query().Get("q"),
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, page)
}

// GetResult godoc
//
//	@Summary		Get one diagnostic result
//	@Description	Admin only. Same as a list item plus every answer and the test's questions with their correct answers.
//	@Tags			admin-diagnostic
//	@Produce		json
//	@Param			id	path		string	true	"Diagnostic result ID"
//	@Success		200	{object}	utils.JSONResponse{data=models.AdminDiagnosticResultDetail}	"Result"
//	@Failure		401	{object}	utils.JSONResponse										"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse										"Not an admin"
//	@Failure		404	{object}	utils.JSONResponse										"Result not found"
//	@Failure		500	{object}	utils.JSONResponse										"Internal server error"
//	@Router			/admin/diagnostic/results/{id} [get]
func (h *DiagnosticHandler) GetResult(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	result, err := h.diagnosticService.GetResult(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, result)
}
