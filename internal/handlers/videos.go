package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const maxVideoRequestBytes = 4 << 10 // 4 KB

type VideoHandler struct {
	videoService *service.VideoService
}

func NewVideoHandler(videoService *service.VideoService) *VideoHandler {
	return &VideoHandler{videoService: videoService}
}

type requestVideoUploadBody struct {
	FileName string `json:"fileName"`
	IsFree   bool   `json:"isFree"`
}

// VideoUploadResponse is a new video plus the signed URL to upload its file to.
type VideoUploadResponse struct {
	Video     *models.Video     `json:"video"`
	UploadURL string            `json:"uploadUrl"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

// VideoResponse wraps one video.
type VideoResponse struct {
	Video *models.Video `json:"video"`
}

// RequestVideoUpload godoc
//
//	@Summary		Start a video upload on a lesson
//	@Description	Course owner (instructor or admin) gets a signed URL to PUT a video file (mp4, mov, mkv or webm, max 5 GB) straight to storage. Send the returned headers unchanged with the PUT, then call POST /videos/{id}/confirm.
//	@Tags			videos
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Lesson ID"
//	@Param			body	body		requestVideoUploadBody							true	"File name and whether the video is free to watch"
//	@Success		201		{object}	utils.JSONResponse{data=VideoUploadResponse}	"Upload started"
//	@Failure		400		{object}	utils.JSONResponse								"Invalid body or unsupported file name"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse								"Not an instructor"
//	@Failure		404		{object}	utils.JSONResponse								"Lesson not found, or not in a course this user teaches"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Failure		503		{object}	utils.JSONResponse								"File storage is disabled (GCS_ENABLE is not true)"
//	@Router			/lessons/{id}/videos [post]
func (h *VideoHandler) RequestUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxVideoRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body requestVideoUploadBody
	if err := dec.Decode(&body); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	up, err := h.videoService.RequestUpload(r.Context(), user, r.PathValue("id"), body.FileName, body.IsFree)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, VideoUploadResponse{
		Video:     up.Video,
		UploadURL: up.URL,
		Method:    http.MethodPut,
		Headers:   up.Headers,
		ExpiresAt: up.ExpiresAt,
	})
}

// ConfirmVideoUpload godoc
//
//	@Summary		Confirm a video upload
//	@Description	Call after the file was PUT to the signed URL. Checks the file and starts transcoding in the background; poll GET /videos/{id} until status is ready.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string										true	"Video ID"
//	@Success		202	{object}	utils.JSONResponse{data=VideoResponse}		"Transcoding queued"
//	@Failure		400	{object}	utils.JSONResponse							"File not uploaded yet, empty, or wrong type"
//	@Failure		401	{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse							"Not an instructor"
//	@Failure		404	{object}	utils.JSONResponse							"Video not found"
//	@Failure		409	{object}	utils.JSONResponse							"Video is not awaiting upload"
//	@Failure		500	{object}	utils.JSONResponse							"Internal server error"
//	@Failure		503	{object}	utils.JSONResponse							"Storage disabled, or transcoding queue busy (retry)"
//	@Router			/videos/{id}/confirm [post]
func (h *VideoHandler) ConfirmUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	video, err := h.videoService.ConfirmUpload(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusAccepted, VideoResponse{Video: video})
}

// RetryVideo godoc
//
//	@Summary		Retry transcoding of a failed video
//	@Description	Starts a new transcoding job for a video whose status is failed. The uploaded file is reused. Poll GET /videos/{id} until status is ready or failed.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string									true	"Video ID"
//	@Success		202	{object}	utils.JSONResponse{data=VideoResponse}	"Transcoding queued again"
//	@Failure		400	{object}	utils.JSONResponse						"Source file is missing"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse						"Not an instructor"
//	@Failure		404	{object}	utils.JSONResponse						"Video not found"
//	@Failure		409	{object}	utils.JSONResponse						"Video is not failed"
//	@Failure		500	{object}	utils.JSONResponse						"Internal server error"
//	@Failure		503	{object}	utils.JSONResponse						"Storage disabled, or transcoding queue busy (retry)"
//	@Router			/videos/{id}/retry [post]
func (h *VideoHandler) RetryTranscode(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	video, err := h.videoService.RetryTranscode(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusAccepted, VideoResponse{Video: video})
}

// GetVideo godoc
//
//	@Summary		Get a video's upload and transcoding status
//	@Description	The uploader (who must still own the course) polls this until status is ready or failed.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string									true	"Video ID"
//	@Success		200	{object}	utils.JSONResponse{data=VideoResponse}	"Video"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse						"Not an instructor"
//	@Failure		404	{object}	utils.JSONResponse						"Video not found"
//	@Failure		500	{object}	utils.JSONResponse						"Internal server error"
//	@Router			/videos/{id} [get]
func (h *VideoHandler) GetVideo(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	video, err := h.videoService.GetVideo(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, VideoResponse{Video: video})
}
