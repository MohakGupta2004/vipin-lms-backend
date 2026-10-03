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
	InstructorID     string `json:"instructorId"` // optional: defaults to the admin creating the course
	Title            string `json:"title"`
	Slug             string `json:"slug"`
	ShortDescription string `json:"shortDescription"`
	Description      string `json:"description"`
	Status           string `json:"status"` // draft (default), published or archived
	IsFree           bool   `json:"isFree"`
}

// UpdateCourseRequest changes only the fields that are sent.
type UpdateCourseRequest struct {
	ExamID           *string `json:"examId"`
	Title            *string `json:"title"`
	Slug             *string `json:"slug"`
	ShortDescription *string `json:"shortDescription"`
	Description      *string `json:"description"`
	IsFree           *bool   `json:"isFree"`
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
//	@Description	Admin only. Creates a course. instructorId is optional and defaults to the admin, who then owns and manages the course; it may also name an active instructor or admin. Status defaults to draft.
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
//	@Description	Course owner only: the instructor or admin whose id is the course's instructorId.
//	@Tags			courses
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Course ID"
//	@Param			request	body		UpdateCourseStatusRequest				true	"New status"
//	@Success		200		{object}	utils.JSONResponse{data=models.Course}	"Course updated"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid status"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor or admin"
//	@Failure		404		{object}	utils.JSONResponse						"Course not found, or not owned by this user"
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

// GetCourse godoc
//
//	@Summary		Get a course
//	@Description	Admins can read any course and owners their own (drafts included). A student can read a published course they have a valid enrollment in. Everyone else gets 404.
//	@Tags			courses
//	@Produce		json
//	@Param			id	path		string									true	"Course ID"
//	@Success		200	{object}	utils.JSONResponse{data=models.Course}	"Course"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		404	{object}	utils.JSONResponse						"Course not found, or not visible to this user"
//	@Failure		500	{object}	utils.JSONResponse						"Internal server error"
//	@Router			/courses/{id} [get]
func (h *CourseHandler) GetCourse(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	course, err := h.courseService.GetCourse(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, course)
}

// ListMyCourses godoc
//
//	@Summary		List my courses
//	@Description	Instructors and admins get the courses they own, drafts included. Students get the published courses they have a valid enrollment in. Newest first.
//	@Tags			courses
//	@Produce		json
//	@Param			limit	query		int											false	"Max courses to return (default 20, max 100)"
//	@Param			offset	query		int											false	"Number of courses to skip (default 0)"
//	@Success		200		{object}	utils.JSONResponse{data=[]models.Course}	"Courses"
//	@Failure		400		{object}	utils.JSONResponse							"Invalid limit or offset"
//	@Failure		401		{object}	utils.JSONResponse							"Not logged in"
//	@Failure		500		{object}	utils.JSONResponse							"Internal server error"
//	@Router			/me/courses [get]
func (h *CourseHandler) ListMyCourses(w http.ResponseWriter, r *http.Request) {
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

	courses, err := h.courseService.ListMyCourses(r.Context(), user, limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, courses)
}

// UpdateCourse godoc
//
//	@Summary		Edit a course
//	@Description	Course owner only. Changes only the fields that are sent. Status has its own endpoint.
//	@Tags			courses
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Course ID"
//	@Param			request	body		UpdateCourseRequest						true	"Fields to change"
//	@Success		200		{object}	utils.JSONResponse{data=models.Course}	"Course updated"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor or admin"
//	@Failure		404		{object}	utils.JSONResponse						"Course not found, or not owned by this user"
//	@Failure		409		{object}	utils.JSONResponse						"Slug already used"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/courses/{id} [patch]
func (h *CourseHandler) UpdateCourse(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCourseBodyBytes)
	var req UpdateCourseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	course, err := h.courseService.UpdateCourse(r.Context(), user, r.PathValue("id"), service.UpdateCourseInput{
		ExamID:           req.ExamID,
		Title:            req.Title,
		Slug:             req.Slug,
		ShortDescription: req.ShortDescription,
		Description:      req.Description,
		IsFree:           req.IsFree,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, course)
}

// DeleteCourse godoc
//
//	@Summary		Delete a course
//	@Description	Course owner only. Soft delete: the course disappears from every list and students lose access. Enrollments are kept unchanged.
//	@Tags			courses
//	@Produce		json
//	@Param			id	path		string							true	"Course ID"
//	@Success		200	{object}	utils.JSONResponse{data=string}	"Course deleted"
//	@Failure		401	{object}	utils.JSONResponse				"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse				"Not an instructor or admin"
//	@Failure		404	{object}	utils.JSONResponse				"Course not found, or not owned by this user"
//	@Failure		500	{object}	utils.JSONResponse				"Internal server error"
//	@Router			/courses/{id} [delete]
func (h *CourseHandler) DeleteCourse(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	if err := h.courseService.DeleteCourse(r.Context(), user, r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "course deleted")
}
