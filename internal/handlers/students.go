package handlers

import (
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

type StudentHandler struct {
	studentService *service.StudentService
}

func NewStudentHandler(studentService *service.StudentService) *StudentHandler {
	return &StudentHandler{studentService: studentService}
}

// ListStudents godoc
//
//	@Summary		List the students of my courses
//	@Description	Instructors and admins. Every student enrolled (not cancelled) in a course the caller owns, in name order, with their progress overall (totals) and per course: videos completed and in progress, approximate watch time, practice sets and mock tests attempted, mock tests passed and the average best mock score, flashcards reviewed, enrollment status and tier, and when they were last active. Students of other instructors' courses are never included.
//	@Tags			instructor
//	@Produce		json
//	@Param			courseId	query		string												false	"Only this course"
//	@Success		200			{object}	utils.JSONResponse{data=[]service.StudentProgress}	"Students"
//	@Failure		400			{object}	utils.JSONResponse									"Invalid courseId"
//	@Failure		401			{object}	utils.JSONResponse									"Not logged in"
//	@Failure		403			{object}	utils.JSONResponse									"Not an instructor or admin"
//	@Failure		500			{object}	utils.JSONResponse									"Internal server error"
//	@Router			/instructor/students [get]
func (h *StudentHandler) ListStudents(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	students, err := h.studentService.ListStudents(r.Context(), user, r.URL.Query().Get("courseId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, students)
}

// GetStudent godoc
//
//	@Summary		One student of my courses
//	@Description	Instructors and admins. The student's progress in the caller's courses (as in the list), the videos they played (most recent first) and up to 100 submitted test attempts (newest first). 404 unless the student is enrolled in a course the caller owns.
//	@Tags			instructor
//	@Produce		json
//	@Param			id	path		string											true	"Student ID"
//	@Success		200	{object}	utils.JSONResponse{data=service.StudentDetail}	"Student"
//	@Failure		401	{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse								"Not an instructor or admin"
//	@Failure		404	{object}	utils.JSONResponse								"Not a student of the caller's courses"
//	@Failure		500	{object}	utils.JSONResponse								"Internal server error"
//	@Router			/instructor/students/{id} [get]
func (h *StudentHandler) GetStudent(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	student, err := h.studentService.GetStudent(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, student)
}
