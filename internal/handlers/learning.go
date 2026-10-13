package handlers

import (
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

type LearningHandler struct {
	learningService *service.LearningService
}

func NewLearningHandler(learningService *service.LearningService) *LearningHandler {
	return &LearningHandler{learningService: learningService}
}

// ListMyVideos godoc
//
//	@Summary		List the videos of my courses
//	@Description	Every ready video across the user's courses (a student's valid enrollments, or the courses an instructor or admin owns), in course and lesson order, each with courseTitle, lessonTitle and the user's own progress (nil until played). Students only get videos of published lessons.
//	@Tags			me
//	@Produce		json
//	@Param			courseId	query		string										false	"Only this course"
//	@Success		200			{object}	utils.JSONResponse{data=[]models.MyVideo}	"Videos"
//	@Failure		400			{object}	utils.JSONResponse							"Invalid courseId"
//	@Failure		401			{object}	utils.JSONResponse							"Not logged in"
//	@Failure		500			{object}	utils.JSONResponse							"Internal server error"
//	@Router			/me/videos [get]
func (h *LearningHandler) ListMyVideos(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	videos, err := h.learningService.ListVideos(r.Context(), user, r.URL.Query().Get("courseId"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, videos)
}

// ListMyQuizzes godoc
//
//	@Summary		List the quizzes of my courses
//	@Description	Every quiz across the user's courses, in course and lesson order, each with courseTitle, lessonTitle and the user's attempt summary: attemptCount, lastAttemptAt, and for mock tests bestPercent and passed. Students only get published quizzes of published lessons.
//	@Tags			me
//	@Produce		json
//	@Param			courseId	query		string										false	"Only this course"
//	@Param			type		query		string										false	"mock_test or practice"
//	@Success		200			{object}	utils.JSONResponse{data=[]models.MyQuiz}	"Quizzes"
//	@Failure		400			{object}	utils.JSONResponse							"Invalid courseId or type"
//	@Failure		401			{object}	utils.JSONResponse							"Not logged in"
//	@Failure		500			{object}	utils.JSONResponse							"Internal server error"
//	@Router			/me/quizzes [get]
func (h *LearningHandler) ListMyQuizzes(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	q := r.URL.Query()
	quizzes, err := h.learningService.ListQuizzes(r.Context(), user, q.Get("courseId"), q.Get("type"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, quizzes)
}

// GetMyProgress godoc
//
//	@Summary		My learning progress
//	@Description	Counts across the user's courses, overall (totals) and per course: videos completed and in progress, approximate watch time, practice sets and mock tests attempted, mock tests passed and the average best mock score, notes available, and an overall percent (videos completed plus quizzes attempted, out of all of them). Also up to 4 started videos to continue, most recent first.
//	@Tags			me
//	@Produce		json
//	@Success		200	{object}	utils.JSONResponse{data=service.ProgressSummary}	"Progress"
//	@Failure		401	{object}	utils.JSONResponse									"Not logged in"
//	@Failure		500	{object}	utils.JSONResponse									"Internal server error"
//	@Router			/me/progress [get]
func (h *LearningHandler) GetMyProgress(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	summary, err := h.learningService.Progress(r.Context(), user)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, summary)
}
