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

// VideosResponse wraps a list of videos.
type VideosResponse struct {
	Videos []models.Video `json:"videos"`
}

// VideoStreamResponse is a signed CDN URL for a video's HLS manifest.
type VideoStreamResponse struct {
	ManifestURL string    `json:"manifestUrl"`
	QueryParams string    `json:"queryParams"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// StreamVideo godoc
//
//	@Summary		Get a signed streaming URL for a video
//	@Description	Returns a signed HLS manifest URL, valid for 15 minutes, that any HLS player (hls.js, Safari, VLC, the browser address bar) can open as-is: every playlist and segment URL inside it is already signed. queryParams is the bare signature. Refetch before expiresAt. Free videos in published courses are open to any logged-in user; everything else needs the course owner or an enrolled student.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string										true	"Video ID"
//	@Success		200	{object}	utils.JSONResponse{data=VideoStreamResponse}	"Signed URL"
//	@Failure		401	{object}	utils.JSONResponse							"Not logged in"
//	@Failure		404	{object}	utils.JSONResponse							"Video not found, or not visible to this user"
//	@Failure		409	{object}	utils.JSONResponse							"Video is not ready yet (owner only)"
//	@Failure		500	{object}	utils.JSONResponse							"Internal server error"
//	@Failure		503	{object}	utils.JSONResponse							"Storage disabled"
//	@Router			/videos/{id}/stream [get]
func (h *VideoHandler) StreamVideo(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	id := r.PathValue("id")
	st, err := h.videoService.StreamVideo(r.Context(), user, id, playlistBaseURL(r, id))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	utils.WriteJSONResponse(w, http.StatusOK, VideoStreamResponse{
		ManifestURL: st.ManifestURL,
		QueryParams: st.QueryParams,
		ExpiresAt:   st.ExpiresAt,
	})
}

// VideoPlaylist godoc
//
//	@Summary		Get a signed HLS playlist
//	@Description	Public: the signature in the query (from GET /videos/{id}/stream) is the credential. Returns the .m3u8 with every URI rewritten to a signed URL: child playlists point back here, segments go straight to the CDN. Open to any origin.
//	@Tags			videos
//	@Produce		application/vnd.apple.mpegurl
//	@Param			id			path		string	true	"Video ID"
//	@Param			file		path		string	true	"Playlist file, e.g. manifest.m3u8"
//	@Param			URLPrefix	query		string	true	"From stream URL"
//	@Param			Expires		query		string	true	"From stream URL"
//	@Param			KeyName		query		string	true	"From stream URL"
//	@Param			Signature	query		string	true	"From stream URL"
//	@Success		200			{string}	string	"Playlist"
//	@Failure		403			{object}	utils.JSONResponse	"Invalid or expired signature"
//	@Failure		404			{object}	utils.JSONResponse	"Video or playlist not found"
//	@Router			/videos/{id}/hls/{file} [get]
func (h *VideoHandler) VideoPlaylist(w http.ResponseWriter, r *http.Request) {
	// Any site may load the playlist; the signature, not cookies, grants access.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Del("Access-Control-Allow-Credentials")
	w.Header().Set("Cache-Control", "no-store")

	id := r.PathValue("id")
	data, err := h.videoService.VideoPlaylist(r.Context(), id, r.PathValue("file"), playlistBaseURL(r, id), r.URL.Query())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Write(data)
}

// playlistBaseURL is this API's absolute URL for a video's playlists, built from the request
// so it works on localhost and behind a proxy that sets X-Forwarded-Proto.
func playlistBaseURL(r *http.Request, videoID string) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/api/v1/videos/" + videoID + "/hls/"
}

// ListLessonVideos godoc
//
//	@Summary		List a lesson's videos
//	@Description	Owner sees every video. Students see ready videos of published lessons; users not enrolled see only free ones.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string									true	"Lesson ID"
//	@Success		200	{object}	utils.JSONResponse{data=VideosResponse}	"Videos"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		404	{object}	utils.JSONResponse						"Lesson not found, or not visible to this user"
//	@Failure		500	{object}	utils.JSONResponse						"Internal server error"
//	@Router			/lessons/{id}/videos [get]
func (h *VideoHandler) ListLessonVideos(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	videos, err := h.videoService.ListLessonVideos(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, VideosResponse{Videos: videos})
}

// ListCourseVideos godoc
//
//	@Summary		List a course's videos
//	@Description	Owner sees every video. Students see ready videos of published lessons; users not enrolled see only free ones.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string									true	"Course ID"
//	@Success		200	{object}	utils.JSONResponse{data=VideosResponse}	"Videos"
//	@Failure		401	{object}	utils.JSONResponse						"Not logged in"
//	@Failure		404	{object}	utils.JSONResponse						"Course not found, or not visible to this user"
//	@Failure		500	{object}	utils.JSONResponse						"Internal server error"
//	@Router			/courses/{id}/videos [get]
func (h *VideoHandler) ListCourseVideos(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	videos, err := h.videoService.ListCourseVideos(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, VideosResponse{Videos: videos})
}
