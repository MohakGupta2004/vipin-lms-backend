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

type SaveDiagnosticResultRequest struct {
	Correct  int                              `json:"correct"`
	Total    int                              `json:"total"`
	Elements []models.DiagnosticElementResult `json:"elements"`
	Answers  []models.DiagnosticAnswer        `json:"answers"`
}

type DiagnosticHandler struct {
	diagnosticService *service.DiagnosticService
}

func NewDiagnosticHandler(diagnosticService *service.DiagnosticService) *DiagnosticHandler {
	return &DiagnosticHandler{diagnosticService: diagnosticService}
}

// SaveResult godoc
//
//	@Summary		Save a diagnostic result
//	@Description	Any logged-in user. Stores the score, the nine element results and up to 100 answers of a completed diagnostic test.
//	@Tags			diagnostic
//	@Accept			json
//	@Produce		json
//	@Param			request	body		SaveDiagnosticResultRequest					true	"Diagnostic result"
//	@Success		201		{object}	utils.JSONResponse{data=models.DiagnosticResult}	"Saved"
//	@Failure		400		{object}	utils.JSONResponse							"Malformed payload or invalid result"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
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
		Correct:  req.Correct,
		Total:    req.Total,
		Elements: req.Elements,
		Answers:  req.Answers,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, result)
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
//	@Description	Admin only. Same as a list item plus every answer.
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
