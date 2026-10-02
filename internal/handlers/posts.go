package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const (
	maxPostBodyBytes = 64 << 10 // 64 KB is plenty for text + 10 links
	defaultFeedLimit = 20
	maxFeedLimit     = 50
)

type CreatePostRequest struct {
	CourseID string   `json:"courseId"`
	Content  string   `json:"content"`
	Links    []string `json:"links"`
}

type PostHandler struct {
	postService *service.PostService
}

func NewPostHandler(postService *service.PostService) *PostHandler {
	return &PostHandler{
		postService: postService,
	}
}

// CreatePost godoc
//
//	@Summary		Create a post
//	@Description	Instructor creates a post (with optional links) for one of their own courses.
//	@Tags			posts
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreatePostRequest						true	"Post details"
//	@Success		201		{object}	utils.JSONResponse{data=models.Post}	"Post created"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or invalid fields"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor of this course"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/posts [post]
func (h *PostHandler) CreatePost(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	// Stop very large bodies before reading them.
	r.Body = http.MaxBytesReader(w, r.Body, maxPostBodyBytes)
	var req CreatePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return
	}

	post, err := h.postService.CreatePost(r.Context(), user, req.CourseID, req.Content, req.Links)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, post)
}

// ListFeed godoc
//
//	@Summary		List my feed
//	@Description	Returns posts from free courses, courses the user is enrolled in, and courses the user teaches. Newest first.
//	@Tags			posts
//	@Produce		json
//	@Param			limit	query		int										false	"Max posts to return (default 20, max 50)"
//	@Param			offset	query		int										false	"Number of posts to skip (default 0)"
//	@Success		200		{object}	utils.JSONResponse{data=[]models.Post}	"Posts"
//	@Failure		400		{object}	utils.JSONResponse						"Invalid limit or offset"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/posts [get]
func (h *PostHandler) ListFeed(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	limit, err := intQuery(r, "limit", defaultFeedLimit)
	if err != nil || limit < 1 || limit > maxFeedLimit {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "limit must be between 1 and 50")
		return
	}
	offset, err := intQuery(r, "offset", 0)
	if err != nil || offset < 0 {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "offset must be 0 or more")
		return
	}

	posts, err := h.postService.ListFeed(r.Context(), user, limit, offset)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, posts)
}

// DeletePost godoc
//
//	@Summary		Delete a post
//	@Description	Instructor deletes a post they created. Its links are deleted too.
//	@Tags			posts
//	@Produce		json
//	@Param			id	path		string							true	"Post ID"
//	@Success		200	{object}	utils.JSONResponse{data=string}	"Post deleted"
//	@Failure		401	{object}	utils.JSONResponse				"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse				"Not an instructor"
//	@Failure		404	{object}	utils.JSONResponse				"Post not found"
//	@Failure		500	{object}	utils.JSONResponse				"Internal server error"
//	@Router			/posts/{id} [delete]
func (h *PostHandler) DeletePost(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	err := h.postService.DeletePost(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "post deleted")
}

// writeServiceError turns a service error into the right HTTP response.
// Unknown errors are logged and hidden from the client so no database details leak.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		utils.WriteJSONResponse(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrForbidden):
		utils.WriteJSONResponse(w, http.StatusForbidden, err.Error())
	case errors.Is(err, service.ErrPostNotFound),
		errors.Is(err, service.ErrCourseNotFound),
		errors.Is(err, service.ErrEnrollmentNotFound):
		utils.WriteJSONResponse(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrSlugTaken), errors.Is(err, service.ErrAlreadyEnrolled):
		utils.WriteJSONResponse(w, http.StatusConflict, err.Error())
	default:
		slog.Error("request failed", "err", err)
		utils.WriteJSONResponse(w, http.StatusInternalServerError, "internal server error")
	}
}

// intQuery reads an integer query param, or returns fallback when it is missing.
func intQuery(r *http.Request, name string, fallback int) (int, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}
