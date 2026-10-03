package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const maxLessonBodyBytes = 256 << 10 // 256 KB: room for a 50,000 character article

type CreateLessonRequest struct {
	Title       string `json:"title"`
	Content     string `json:"content"` // short description of the chapter
	IsFree      bool   `json:"isFree"`
	IsPublished *bool  `json:"isPublished"` // defaults to true
}

// UpdateLessonRequest changes only the fields that are sent.
type UpdateLessonRequest struct {
	Title       *string `json:"title"`
	Content     *string `json:"content"`
	IsFree      *bool   `json:"isFree"`
	IsPublished *bool   `json:"isPublished"`
}

type LessonHandler struct {
	lessonService *service.LessonService
}

func NewLessonHandler(lessonService *service.LessonService) *LessonHandler {
	return &LessonHandler{
		lessonService: lessonService,
	}
}

// CreateLesson godoc
//
//	@Summary		Create a lesson (chapter)
//	@Description	Course owner (instructor or admin) adds a lesson to their course. A lesson is a chapter: add as many PDF notes to it as needed with POST /lessons/{id}/notes. It goes at the end of the course and is published by default.
//	@Tags			lessons
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Course ID"
//	@Param			request	body		CreateLessonRequest						true	"Lesson details"
//	@Success		201		{object}	utils.JSONResponse{data=models.Lesson}	"Lesson created"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not the instructor of this course"
//	@Failure		404		{object}	utils.JSONResponse						"Course not found"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/courses/{id}/lessons [post]
func (h *LessonHandler) CreateLesson(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLessonBodyBytes)
	var req CreateLessonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}
	isPublished := true
	if req.IsPublished != nil {
		isPublished = *req.IsPublished
	}

	lesson, err := h.lessonService.CreateLesson(r.Context(), user, r.PathValue("id"), service.CreateLessonInput{
		Title:       req.Title,
		Content:     req.Content,
		IsFree:      req.IsFree,
		IsPublished: isPublished,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, lesson)
}

// ListLessons godoc
//
//	@Summary		List a course's lessons with their notes
//	@Description	Returns the course's lessons (chapters) in order, each with its PDF notes. Only the course instructor and enrolled students can see it. Students see published lessons only.
//	@Tags			lessons
//	@Produce		json
//	@Param			id	path		string										true	"Course ID"
//	@Success		200	{object}	utils.JSONResponse{data=[]models.Lesson}	"Lessons with notes"
//	@Failure		401	{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse							"Not enrolled in this course"
//	@Failure		404	{object}	utils.JSONResponse							"Course not found"
//	@Failure		500	{object}	utils.JSONResponse							"Internal server error"
//	@Router			/courses/{id}/lessons [get]
func (h *LessonHandler) ListLessons(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	lessons, err := h.lessonService.ListLessons(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, lessons)
}

// UpdateLesson godoc
//
//	@Summary		Edit a lesson
//	@Description	Course owner (instructor or admin) changes only the fields that are sent. Send isPublished to publish or unpublish the lesson at any time.
//	@Tags			lessons
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Lesson ID"
//	@Param			request	body		UpdateLessonRequest						true	"Fields to change"
//	@Success		200		{object}	utils.JSONResponse{data=models.Lesson}	"Lesson updated"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor or admin"
//	@Failure		404		{object}	utils.JSONResponse						"Lesson not found, or not in a course this user owns"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/lessons/{id} [patch]
func (h *LessonHandler) UpdateLesson(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxLessonBodyBytes)
	var req UpdateLessonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	lesson, err := h.lessonService.UpdateLesson(r.Context(), user, r.PathValue("id"), service.UpdateLessonInput{
		Title:       req.Title,
		Content:     req.Content,
		IsFree:      req.IsFree,
		IsPublished: req.IsPublished,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, lesson)
}

// DeleteLesson godoc
//
//	@Summary		Delete a lesson
//	@Description	Course owner (instructor or admin) deletes a lesson. Its notes and their PDF files are removed. Its quizzes are hidden, but students' past attempts are kept.
//	@Tags			lessons
//	@Produce		json
//	@Param			id	path		string							true	"Lesson ID"
//	@Success		200	{object}	utils.JSONResponse{data=string}	"Lesson deleted"
//	@Failure		401	{object}	utils.JSONResponse				"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse				"Not an instructor or admin"
//	@Failure		404	{object}	utils.JSONResponse				"Lesson not found, or not in a course this user owns"
//	@Failure		503	{object}	utils.JSONResponse				"Lesson has PDF notes and PDF storage is disabled"
//	@Failure		500	{object}	utils.JSONResponse				"Internal server error"
//	@Router			/lessons/{id} [delete]
func (h *LessonHandler) DeleteLesson(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	if err := h.lessonService.DeleteLesson(r.Context(), user, r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "lesson deleted")
}
