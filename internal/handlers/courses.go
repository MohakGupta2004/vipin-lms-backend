package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	maxCourseBodyBytes  = 64 << 10 // 64 KB
	defaultCoursesLimit = 20
	maxCoursesLimit     = 100
)

type CreateCourseRequest struct {
	ExamID           string `json:"examId"`
	InstructorID     string `json:"instructorId"`
	Title            string `json:"title"`
	Slug             string `json:"slug"`
	ShortDescription string `json:"shortDescription"`
	Description      string `json:"description"`
	Status           string `json:"status"` // draft (default), published or archived
	IsFree           bool   `json:"isFree"`
}

type UpdateCourseStatusRequest struct {
	Status string `json:"status"` // draft, published or archived
}

type CourseHandler struct {
	courseService *service.CourseService
}

func NewCourseHandler(courseService *service.CourseService) *CourseHandler {
	return &CourseHandler{
		courseService: courseService,
	}
}

// CreateCourse godoc
//
//	@Summary		Create a course
//	@Description	Admin only. Creates a course for an instructor. Status defaults to draft.
//	@Tags			courses
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreateCourseRequest							true	"Course details"
//	@Success		201		{object}	utils.JSONResponse{data=models.Course}	"Course created"
//	@Failure		400		{object}	utils.JSONResponse							"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse							"Not an admin"
//	@Failure		409		{object}	utils.JSONResponse							"Slug already used"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/courses [post]
func (h *CourseHandler) CreateCourse(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCourseBodyBytes)
	var req CreateCourseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	course, err := h.courseService.CreateCourse(r.Context(), user, service.CreateCourseInput{
		ExamID:           req.ExamID,
		InstructorID:     req.InstructorID,
		Title:            req.Title,
		Slug:             req.Slug,
		ShortDescription: req.ShortDescription,
		Description:      req.Description,
		Status:           req.Status,
		IsFree:           req.IsFree,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, course)
}

// ListCourses godoc
//
//	@Summary		List courses
//	@Description	Admin only. Returns all courses that are not deleted, newest first.
//	@Tags			courses
//	@Produce		json
//	@Param			limit	query		int											false	"Max courses to return (default 20, max 100)"
//	@Param			offset	query		int											false	"Number of courses to skip (default 0)"
//	@Success		200		{object}	utils.JSONResponse{data=[]models.Course}	"Courses"
//	@Failure		400		{object}	utils.JSONResponse							"Invalid limit or offset"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse							"Not an admin"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/courses [get]
func (h *CourseHandler) ListCourses(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", defaultCoursesLimit)
	if err != nil || limit < 1 || limit > maxCoursesLimit {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be between 1 and 100")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil || offset < 0 {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be 0 or more")
		return
	}

	courses, err := h.courseService.ListCourses(r.Context(), user, limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, courses)
}

// UpdateCourseStatus godoc
//
//	@Summary		Change course status
//	@Description	Instructor only. Changes the status of a course the instructor teaches.
//	@Tags			courses
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Course ID"
//	@Param			request	body		UpdateCourseStatusRequest				true	"New status"
//	@Success		200		{object}	utils.JSONResponse{data=models.Course}	"Course updated"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid status"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor"
//	@Failure		404		{object}	utils.JSONResponse						"Course not found"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/courses/{id}/status [patch]
func (h *CourseHandler) UpdateCourseStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCourseBodyBytes)
	var req UpdateCourseStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	course, err := h.courseService.UpdateCourseStatus(r.Context(), user, r.PathValue("id"), req.Status)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, course)
}
