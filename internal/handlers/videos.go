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

const maxVideoRequestBytes = 32 << 10 // 32 KB, room for a 5000-character description

type VideoHandler struct {
	videoService *service.VideoService
}

func NewVideoHandler(videoService *service.VideoService) *VideoHandler {
	return &VideoHandler{videoService: videoService}
}

type requestVideoUploadBody struct {
	FileName    string   `json:"fileName"`
	Description string   `json:"description"` // optional, what the lecture covers
	Kind        string   `json:"kind"`        // offline (default) or meet
	Tiers       []string `json:"tiers"`       // free, plus, premium; defaults by kind; ultra is always included
	IsFree      bool     `json:"isFree"`      // deprecated: true is the same as tiers [free]
}

// updateVideoBody holds the video fields to change; omitted fields stay as they are.
type updateVideoBody struct {
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Kind        *string  `json:"kind"`   // offline or meet
	Tiers       []string `json:"tiers"`  // free, plus, premium; ultra is always included
	IsFree      *bool    `json:"isFree"` // deprecated: use tiers
}

// VideoUploadResponse is a video waiting for its HLS files plus the signed form to upload them with.
// Each file is POSTed to uploadUrl as multipart/form-data: the fields for its extension, then "key"
// (keyPrefix + the file's path, e.g. keyPrefix + "720p/seg-00001.m4s"), then "file" last.
type VideoUploadResponse struct {
	Video        *models.Video                `json:"video"`
	UploadURL    string                       `json:"uploadUrl"`
	Method       string                       `json:"method"`
	KeyPrefix    string                       `json:"keyPrefix"`
	Fields       map[string]map[string]string `json:"fields"` // per extension: .m3u8, .mp4 (init segments), .m4s
	MaxFileBytes int64                        `json:"maxFileBytes"`
	ExpiresAt    time.Time                    `json:"expiresAt"`
}

func newVideoUploadResponse(up *service.VideoUpload) VideoUploadResponse {
	return VideoUploadResponse{
		Video:        up.Video,
		UploadURL:    up.URL,
		Method:       http.MethodPost,
		KeyPrefix:    up.KeyPrefix,
		Fields:       up.Fields,
		MaxFileBytes: up.MaxFileBytes,
		ExpiresAt:    up.ExpiresAt,
	}
}

// VideoResponse wraps one video.
type VideoResponse struct {
	Video *models.Video `json:"video"`
}

// RequestVideoUpload godoc
//
//	@Summary		Start a video upload on a lesson
//	@Description	Course owner (instructor or admin) starts a video (fileName must end in mp4, mov, mkv or webm; it becomes the title). The browser transcodes the file to HLS itself and uploads the files straight to storage with the returned signed form, which only accepts files under keyPrefix and expires at expiresAt (renew it with POST /videos/{id}/upload). Then call POST /videos/{id}/confirm.
//	@Tags			videos
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Lesson ID"
//	@Param			body	body		requestVideoUploadBody							true	"File name, optional description, kind (offline or meet) and the tiers that can watch it"
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

	tiers := body.Tiers
	if tiers == nil && body.IsFree {
		tiers = []string{"free"}
	}
	up, err := h.videoService.RequestUpload(r.Context(), user, r.PathValue("id"), body.FileName, body.Description, body.Kind, tiers)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusCreated, newVideoUploadResponse(up))
}

// RenewVideoUpload godoc
//
//	@Summary		Renew a video's upload form
//	@Description	Returns a fresh signed upload form for a video that is still uploading: when the form is about to expire during a long upload, or to upload the video again after a failed attempt. Also keeps an upload that is still running from being cleaned up as abandoned.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string											true	"Video ID"
//	@Success		200	{object}	utils.JSONResponse{data=VideoUploadResponse}	"New upload form"
//	@Failure		401	{object}	utils.JSONResponse								"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse								"Not an instructor"
//	@Failure		404	{object}	utils.JSONResponse								"Video not found"
//	@Failure		409	{object}	utils.JSONResponse								"Video is not awaiting upload"
//	@Failure		500	{object}	utils.JSONResponse								"Internal server error"
//	@Failure		503	{object}	utils.JSONResponse								"File storage is disabled"
//	@Router			/videos/{id}/upload [post]
func (h *VideoHandler) RenewUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	up, err := h.videoService.RenewUpload(r.Context(), user, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	utils.WriteJSONResponse(w, http.StatusOK, newVideoUploadResponse(up))
}

// ConfirmVideoUpload godoc
//
//	@Summary		Confirm a video upload
//	@Description	Call once every HLS file is uploaded. Checks the files (playlists, every referenced file present with the right type, nothing outside the video's folder), removes stray files from earlier attempts and makes the video ready. If the check fails the video stays uploading, so it can be uploaded again.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string										true	"Video ID"
//	@Success		200	{object}	utils.JSONResponse{data=VideoResponse}		"Video ready"
//	@Failure		400	{object}	utils.JSONResponse							"Files missing, incomplete or invalid"
//	@Failure		401	{object}	utils.JSONResponse							"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse							"Not an instructor"
//	@Failure		404	{object}	utils.JSONResponse							"Video not found"
//	@Failure		409	{object}	utils.JSONResponse							"Video is not awaiting upload"
//	@Failure		500	{object}	utils.JSONResponse							"Internal server error"
//	@Failure		503	{object}	utils.JSONResponse							"Storage disabled"
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
	utils.WriteJSONResponse(w, http.StatusOK, VideoResponse{Video: video})
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

// DeleteVideo godoc
//
//	@Summary		Delete a video
//	@Description	The uploader (who must still own the course) deletes a video. Students' progress on it goes too, and its files are removed from storage. A video that is still processing can't be deleted until it is ready or has failed.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string				true	"Video ID"
//	@Success		200	{object}	utils.JSONResponse	"Video deleted"
//	@Failure		401	{object}	utils.JSONResponse	"Not logged in"
//	@Failure		403	{object}	utils.JSONResponse	"Not an instructor"
//	@Failure		404	{object}	utils.JSONResponse	"Video not found"
//	@Failure		409	{object}	utils.JSONResponse	"Video is still processing"
//	@Failure		500	{object}	utils.JSONResponse	"Internal server error"
//	@Failure		503	{object}	utils.JSONResponse	"Video storage is disabled"
//	@Router			/videos/{id} [delete]
func (h *VideoHandler) DeleteVideo(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	if err := h.videoService.DeleteVideo(r.Context(), user, r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "video deleted")
}

// UpdateVideo godoc
//
//	@Summary		Edit a video's details, section and access
//	@Description	The uploader (who must still own the course) changes the title, the description students see under the player, the section (kind: offline or meet) or the tiers that can watch it. Omitted fields are left as they are; an empty description removes it.
//	@Tags			videos
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string									true	"Video ID"
//	@Param			body	body		updateVideoBody							true	"Fields to change"
//	@Success		200		{object}	utils.JSONResponse{data=VideoResponse}	"Updated video"
//	@Failure		400		{object}	utils.JSONResponse						"Invalid body, empty title, or description too long"
//	@Failure		401		{object}	utils.JSONResponse						"Not logged in"
//	@Failure		403		{object}	utils.JSONResponse						"Not an instructor"
//	@Failure		404		{object}	utils.JSONResponse						"Video not found"
//	@Failure		500		{object}	utils.JSONResponse						"Internal server error"
//	@Router			/videos/{id} [patch]
func (h *VideoHandler) UpdateVideo(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxVideoRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body updateVideoBody
	if err := dec.Decode(&body); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	video, err := h.videoService.UpdateVideo(r.Context(), user, r.PathValue("id"),
		service.UpdateVideoInput{Title: body.Title, Description: body.Description, Kind: body.Kind, Tiers: body.Tiers, IsFree: body.IsFree})
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
	ResumeAt    int       `json:"resumeAt"` // seconds; where the user stopped last time, 0 = from the start
	// NativeAspect is true when the picture already has the recording's own shape. False for older videos,
	// which were stretched to 16:9 and have to be drawn back at their recorded shape.
	NativeAspect bool `json:"nativeAspect"`
}

// StreamVideo godoc
//
//	@Summary		Get a signed streaming URL for a video
//	@Description	Returns a signed HLS manifest URL, valid for 15 minutes, that any HLS player (hls.js, Safari, VLC, the browser address bar) can open as-is: every playlist and segment URL inside it is already signed. queryParams is the bare signature. Refetch before expiresAt. Videos marked free in published courses (a free lesson does not unlock its videos), and every video of a free course, are open to any logged-in user; everything else needs the course owner or an enrolled student.
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
		ManifestURL:  st.ManifestURL,
		QueryParams:  st.QueryParams,
		ExpiresAt:    st.ExpiresAt,
		ResumeAt:     st.ResumeAt,
		NativeAspect: st.NativeAspect,
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
//	@Description	Owner sees every video. Students see ready videos of published lessons. Users not enrolled, or enrolled on a lower tier, see them too, with locked=true on the ones their tier cannot stream.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string									true	"Lesson ID"
//	@Param			kind	query		string									false	"Only videos of this kind: offline or meet"
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
	videos, err := h.videoService.ListLessonVideos(r.Context(), user, r.PathValue("id"), r.URL.Query().Get("kind"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, VideosResponse{Videos: videos})
}

// ListCourseVideos godoc
//
//	@Summary		List a course's videos
//	@Description	Owner sees every video. Students see ready videos of published lessons. Users not enrolled, or enrolled on a lower tier, see them too, with locked=true on the ones their tier cannot stream.
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		string									true	"Course ID"
//	@Param			kind	query		string									false	"Only videos of this kind: offline or meet"
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
	videos, err := h.videoService.ListCourseVideos(r.Context(), user, r.PathValue("id"), r.URL.Query().Get("kind"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, VideosResponse{Videos: videos})
}

type saveVideoProgressBody struct {
	PositionSec int `json:"positionSec"`
	// DurationSec is the video's length as the player reports it. Optional; once sent, reaching 90% of it marks the video completed.
	DurationSec *int `json:"durationSec,omitempty"`
}

type setVideoCompletionBody struct {
	Completed bool `json:"completed"`
}

// VideoCompletionResponse is the saved completion state.
type VideoCompletionResponse struct {
	Completed bool `json:"completed"`
}

// VideoProgressResponse is the saved playback position.
type VideoProgressResponse struct {
	PositionSec int `json:"positionSec"`
}

// SaveVideoProgress godoc
//
//	@Summary		Save where the user stopped in a video
//	@Description	Call every 10-15 seconds while playing and on pause. The next GET /videos/{id}/stream returns it as resumeAt and the manifest starts playback there. Send 0 when the video ends so it restarts from the beginning. Send durationSec too once the player knows it: a position past 90% of it marks the video completed (completion is never undone by a later position).
//	@Tags			videos
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string											true	"Video ID"
//	@Param			body	body		saveVideoProgressBody							true	"Position in seconds (0 to 86400), and optionally the duration (1 to 86400)"
//	@Success		200		{object}	utils.JSONResponse{data=VideoProgressResponse}	"Saved"
//	@Failure		400		{object}	utils.JSONResponse								"Invalid body or position"
//	@Failure		401		{object}	utils.JSONResponse								"Not logged in"
//	@Failure		404		{object}	utils.JSONResponse								"Video not found, or not visible to this user"
//	@Failure		409		{object}	utils.JSONResponse								"Video is not ready yet (owner only)"
//	@Failure		500		{object}	utils.JSONResponse								"Internal server error"
//	@Router			/videos/{id}/progress [put]
func (h *VideoHandler) SaveVideoProgress(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxVideoRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body saveVideoProgressBody
	if err := dec.Decode(&body); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.videoService.SaveProgress(r.Context(), user, r.PathValue("id"), body.PositionSec, body.DurationSec); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, VideoProgressResponse{PositionSec: body.PositionSec})
}

// SetVideoCompletion godoc
//
//	@Summary		Mark a video completed or not completed
//	@Description	For the user's own checklist: the course player's tick box, and the end of playback. Shows up in GET /me/progress and in each video's progress.
//	@Tags			videos
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string												true	"Video ID"
//	@Param			body	body		setVideoCompletionBody								true	"Completed or not"
//	@Success		200		{object}	utils.JSONResponse{data=VideoCompletionResponse}	"Saved"
//	@Failure		400		{object}	utils.JSONResponse									"Invalid body"
//	@Failure		401		{object}	utils.JSONResponse									"Not logged in"
//	@Failure		404		{object}	utils.JSONResponse									"Video not found, or not visible to this user"
//	@Failure		409		{object}	utils.JSONResponse									"Video is not ready yet (owner only)"
//	@Failure		500		{object}	utils.JSONResponse									"Internal server error"
//	@Router			/videos/{id}/completion [put]
func (h *VideoHandler) SetVideoCompletion(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxVideoRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body setVideoCompletionBody
	if err := dec.Decode(&body); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.videoService.SetCompleted(r.Context(), user, r.PathValue("id"), body.Completed); err != nil {
		writeServiceError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, VideoCompletionResponse{Completed: body.Completed})
}
