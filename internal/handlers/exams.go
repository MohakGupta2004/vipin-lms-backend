package handlers

import (
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

type ExamHandler struct {
	examRepo *models.ExamRepository
}

func NewExamHandler(examRepo *models.ExamRepository) *ExamHandler {
	return &ExamHandler{
		examRepo: examRepo,
	}
}

// ListExams godoc
//
//	@Summary		List exams
//	@Description	Returns all active exams, sorted by code. Any logged-in user can call this.
//	@Tags			exams
//	@Produce		json
//	@Success		200	{object}	utils.JSONResponse{data=[]models.Exam}	"Exams"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		500	{object}	utils.JSONResponse						"Internal server error"
//	@Router			/exams [get]
func (h *ExamHandler) ListExams(w http.ResponseWriter, r *http.Request) {
	exams, err := h.examRepo.ListActiveExams(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, exams)
}
