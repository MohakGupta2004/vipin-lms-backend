package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	maxEnrollmentBodyBytes  = 4 << 10 // 4 KB
	defaultEnrollmentsLimit = 20
	maxEnrollmentsLimit     = 100
)

type CreateEnrollmentRequest struct {
	UserID   string `json:"userId"`
	CourseID string `json:"courseId"`
	Months   int    `json:"months"` // how long the enrollment lasts, like 3
}

type UpdateEnrollmentStatusRequest struct {
	Status string `json:"status"` // active, completed, expired or cancelled
}

type EnrollmentHandler struct {
	enrollmentService *service.EnrollmentService
}

func NewEnrollmentHandler(enrollmentService *service.EnrollmentService) *EnrollmentHandler {
	return &EnrollmentHandler{
		enrollmentService: enrollmentService,
	}
}

// CreateEnrollment godoc
//
//	@Summary		Enroll a student
//	@Description	Admin only. Enrolls a student in a published course for a number of months (1 to 60). The expiry date is calculated by the server.
//	@Tags			enrollments
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreateEnrollmentRequest						true	"Enrollment details"
//	@Success		201		{object}	utils.JSONResponse{data=models.Enrollment}	"Enrolled"
//	@Failure		400		{object}	utils.JSONResponse							"Malformed payload, bad student, or course not published"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse							"Not an admin"
//	@Failure		409		{object}	utils.JSONResponse							"Already enrolled"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/enrollments [post]
func (h *EnrollmentHandler) CreateEnrollment(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxEnrollmentBodyBytes)
	var req CreateEnrollmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	enrollment, err := h.enrollmentService.CreateEnrollment(r.Context(), user, service.CreateEnrollmentInput{
		UserID:   req.UserID,
		CourseID: req.CourseID,
		Months:   req.Months,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, enrollment)
}

// ListEnrollments godoc
//
//	@Summary		List enrollments
//	@Description	Admin only. Newest first. Filter by courseId and/or userId.
//	@Tags			enrollments
//	@Produce		json
//	@Param			courseId	query		string										false	"Only this course"
//	@Param			userId		query		string										false	"Only this student"
//	@Param			limit		query		int											false	"Max results (default 20, max 100)"
//	@Param			offset		query		int											false	"Number to skip (default 0)"
//	@Success		200			{object}	utils.JSONResponse{data=[]models.Enrollment}	"Enrollments"
//	@Failure		400			{object}	utils.JSONResponse							"Invalid filter, limit or offset"
//	@Failure		401			{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403			{object}	utils.JSONResponse							"Not an admin"
//	@Failure		500			{object}	utils.JSONResponse							"Internal server error"
//	@Router			/enrollments [get]
func (h *EnrollmentHandler) ListEnrollments(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", defaultEnrollmentsLimit)
	if err != nil || limit < 1 || limit > maxEnrollmentsLimit {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be between 1 and 100")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil || offset < 0 {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be 0 or more")
		return
	}

	query := r.URL.Query()
	enrollments, err := h.enrollmentService.ListEnrollments(r.Context(), user, query.Get("courseId"), query.Get("userId"), limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, enrollments)
}

// UpdateEnrollmentStatus godoc
//
//	@Summary		Change enrollment status
//	@Description	Admin only. For example set an enrollment to cancelled.
//	@Tags			enrollments
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string										true	"Enrollment ID"
//	@Param			request	body		UpdateEnrollmentStatusRequest				true	"New status"
//	@Success		200		{object}	utils.JSONResponse{data=models.Enrollment}	"Enrollment updated"
//	@Failure		400		{object}	utils.JSONResponse							"Malformed payload or invalid status"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse							"Not an admin"
//	@Failure		404		{object}	utils.JSONResponse							"Enrollment not found"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/enrollments/{id} [patch]
func (h *EnrollmentHandler) UpdateEnrollmentStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxEnrollmentBodyBytes)
	var req UpdateEnrollmentStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	enrollment, err := h.enrollmentService.UpdateEnrollmentStatus(r.Context(), user, r.PathValue("id"), req.Status)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, enrollment)
}
