package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/cdn"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
	"github.com/google/uuid"
)

const (
	maxVideoFileNameChars = 255
	// streamURLExpiry is short so a leaked URL stops working soon; clients refetch before expiresAt.
	streamURLExpiry = 15 * time.Minute
	// maxPlaylistBytes caps an .m3u8 file read from storage; real ones are a few KB.
	maxPlaylistBytes = 1 << 20
)

// playlistNamePattern is a playlist file directly inside a video's hls/ folder, e.g. "media-sd.m3u8".
var playlistNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.m3u8$`)

var (
	// ErrVideoNotFound means the video does not exist, or the user may not see it.
	ErrVideoNotFound = errors.New("video not found")
	// ErrVideoNotAwaitingUpload means the video is no longer in the 'uploading' state.
	ErrVideoNotAwaitingUpload = errors.New("video is not awaiting upload")
	// ErrVideoNotFailed means only a failed video can be retried.
	ErrVideoNotFailed = errors.New("only a failed video can be retried")
	// ErrVideoNotReady means the owner asked to stream a video that has not finished transcoding.
	ErrVideoNotReady = errors.New("video is not ready to stream yet")
	// ErrBadStreamSignature means a playlist request has a missing, tampered or expired signature.
	ErrBadStreamSignature = errors.New("stream link is invalid or expired")
	// ErrTranscodeQueueBusy means the video could not be queued for transcoding. The client can retry confirm.
	ErrTranscodeQueueBusy = errors.New("transcoding queue is busy, try again shortly")
)

// TranscodeEnqueuer queues a confirmed video for transcoding.
type TranscodeEnqueuer interface {
	Enqueue(ctx context.Context, videoID string) error
}

type VideoService struct {
	videoRepo  *models.VideoRepository
	lessonRepo *models.LessonRepository
	store      storage.VideoStore
	queue      TranscodeEnqueuer
	signer     *cdn.Signer // nil when GCS is disabled
}

func NewVideoService(videoRepo *models.VideoRepository, lessonRepo *models.LessonRepository, store storage.VideoStore, queue TranscodeEnqueuer, signer *cdn.Signer) *VideoService {
	return &VideoService{videoRepo: videoRepo, lessonRepo: lessonRepo, store: store, queue: queue, signer: signer}
}

// VideoUpload is a new video row plus the signed URL the browser must PUT the file to.
type VideoUpload struct {
	Video     *models.Video
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

// RequestUpload lets the course owner start a video upload on a lesson of their course.
// It creates the video row ('uploading') and returns a signed PUT URL straight to the bucket.
func (s *VideoService) RequestUpload(ctx context.Context, user *models.User, lessonID, fileName string, isFree bool) (*VideoUpload, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if !s.store.Enabled() {
		return nil, ErrStorageDisabled
	}
	lesson, err := loadOwnedLesson(ctx, s.lessonRepo, user, lessonID)
	if err != nil {
		return nil, err
	}
	name, ext, contentType, err := validateVideoFileName(fileName)
	if err != nil {
		return nil, err
	}

	id := uuid.NewString()
	key := path.Join("courses", lesson.CourseID, "videos", id, "source"+ext)

	// Sign before inserting so a signing error leaves no row behind.
	expires := storage.VideoUploadURLExpiry()
	url, headers, err := s.store.SignedUploadURL(key, contentType, storage.MaxVideoSize, expires)
	if err != nil {
		return nil, err
	}
	expiresAt := time.Now().Add(expires)

	video := &models.Video{
		ID:          id,
		LessonID:    lesson.ID,
		CourseID:    lesson.CourseID,
		UploadedBy:  user.ID,
		Title:       name,
		IsFree:      isFree,
		OriginalKey: key,
	}
	if err := s.videoRepo.Create(ctx, video); err != nil {
		return nil, err
	}
	return &VideoUpload{Video: video, URL: url, Headers: headers, ExpiresAt: expiresAt}, nil
}

// ConfirmUpload checks the file reached the bucket and queues it for transcoding.
func (s *VideoService) ConfirmUpload(ctx context.Context, user *models.User, videoID string) (*models.Video, error) {
	video, err := s.ownedVideo(ctx, user, videoID)
	if err != nil {
		return nil, err
	}
	if video.Status != "uploading" {
		return nil, ErrVideoNotAwaitingUpload
	}
	if !s.store.Enabled() {
		return nil, ErrStorageDisabled
	}

	size, contentType, err := s.store.ObjectInfo(ctx, video.OriginalKey)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return nil, fmt.Errorf("%w: video file not uploaded yet", ErrInvalidInput)
	}
	if err != nil {
		return nil, err
	}
	wantType, _ := storage.VideoContentType(path.Ext(video.OriginalKey))
	switch {
	case size <= 0:
		return nil, fmt.Errorf("%w: uploaded video file is empty", ErrInvalidInput)
	case size > storage.MaxVideoSize:
		return nil, fmt.Errorf("%w: video must be at most %d GB", ErrInvalidInput, storage.MaxVideoSize>>30)
	case contentType != wantType:
		return nil, fmt.Errorf("%w: uploaded video has the wrong content type", ErrInvalidInput)
	}

	// Atomic: of two concurrent confirms only one gets past this.
	if err := s.videoRepo.MarkProcessing(ctx, video.ID, size); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVideoNotAwaitingUpload
	} else if err != nil {
		return nil, err
	}

	if err := s.queue.Enqueue(ctx, video.ID); err != nil {
		slog.Error("enqueue transcode failed", "video", video.ID, "err", err)
		revertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if rerr := s.videoRepo.RevertToUploading(revertCtx, video.ID); rerr != nil {
			slog.Error("revert video to uploading failed", "video", video.ID, "err", rerr)
		}
		return nil, ErrTranscodeQueueBusy
	}

	video.Status = "processing"
	video.SizeBytes = size
	return video, nil
}

// RetryTranscode starts transcoding again for a failed video. The source file stays in the bucket,
// so nothing is re-uploaded; a new Transcoder job replaces the failed one.
func (s *VideoService) RetryTranscode(ctx context.Context, user *models.User, videoID string) (*models.Video, error) {
	video, err := s.ownedVideo(ctx, user, videoID)
	if err != nil {
		return nil, err
	}
	if video.Status != "failed" {
		return nil, ErrVideoNotFailed
	}
	if !s.store.Enabled() {
		return nil, ErrStorageDisabled
	}
	if _, _, err := s.store.ObjectInfo(ctx, video.OriginalKey); errors.Is(err, storage.ErrObjectNotFound) {
		return nil, fmt.Errorf("%w: source video file is missing, upload it again", ErrInvalidInput)
	} else if err != nil {
		return nil, err
	}

	// Atomic: of two concurrent retries only one gets past this.
	if err := s.videoRepo.MarkRetrying(ctx, video.ID); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVideoNotFailed
	} else if err != nil {
		return nil, err
	}

	if err := s.queue.Enqueue(ctx, video.ID); err != nil {
		slog.Error("enqueue transcode retry failed", "video", video.ID, "err", err)
		revertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		if rerr := s.videoRepo.MarkFailed(revertCtx, video.ID, video.Error); rerr != nil {
			slog.Error("revert video to failed failed", "video", video.ID, "err", rerr)
		}
		return nil, ErrTranscodeQueueBusy
	}

	video.Status = "processing"
	video.Error = ""
	return video, nil
}

// GetVideo returns a video to the person who uploaded it, if they still own its course.
func (s *VideoService) GetVideo(ctx context.Context, user *models.User, videoID string) (*models.Video, error) {
	return s.ownedVideo(ctx, user, videoID)
}

// VideoStream is a ready-to-use signed manifest URL. The manifest is served by the API with every
// playlist and segment URL inside already signed, so any HLS player can open it as-is.
// QueryParams is the bare signature, for clients that want to build URLs themselves.
type VideoStream struct {
	ManifestURL string
	QueryParams string
	ExpiresAt   time.Time
}

// StreamVideo returns a short-lived signed URL for a ready video the user may watch.
// playlistBase is the API URL that serves playlists, e.g. "http://localhost:3000/api/v1/videos/{id}/hls/".
func (s *VideoService) StreamVideo(ctx context.Context, user *models.User, videoID, playlistBase string) (*VideoStream, error) {
	if !uuidPattern.MatchString(videoID) {
		return nil, ErrVideoNotFound
	}
	if s.signer == nil {
		return nil, ErrStorageDisabled
	}
	video, err := s.videoRepo.Get(ctx, videoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	if video.LessonID == "" {
		return nil, ErrVideoNotFound
	}
	lesson, err := s.lessonRepo.GetLesson(ctx, video.LessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	isTeacher, freeOnly, err := s.videoAccess(ctx, user, lesson)
	if err != nil {
		return nil, notFoundAs(err, ErrVideoNotFound)
	}
	if freeOnly && !video.IsFree && !lesson.IsFree {
		return nil, ErrVideoNotFound
	}
	if video.Status != "ready" || video.HLSPrefix == "" {
		if !isTeacher {
			return nil, ErrVideoNotFound
		}
		return nil, ErrVideoNotReady
	}

	expires := time.Now().Add(streamURLExpiry)
	query := s.signer.SignPrefix("/"+video.HLSPrefix, expires)
	return &VideoStream{
		ManifestURL: playlistBase + "manifest.m3u8?" + query,
		QueryParams: query,
		ExpiresAt:   expires,
	}, nil
}

// VideoPlaylist returns one of a ready video's HLS playlists with every URI inside rewritten to a signed URL:
// child playlists point back at playlistBase (this API), segments straight at the CDN.
// The signature from StreamVideo is the only credential, so players that do not carry cookies still work.
func (s *VideoService) VideoPlaylist(ctx context.Context, videoID, fileName, playlistBase string, q url.Values) ([]byte, error) {
	if !uuidPattern.MatchString(videoID) || !playlistNamePattern.MatchString(fileName) {
		return nil, ErrVideoNotFound
	}
	if s.signer == nil {
		return nil, ErrStorageDisabled
	}
	signedPrefix, query, err := s.signer.Verify(q, time.Now())
	if err != nil {
		return nil, ErrBadStreamSignature
	}
	video, err := s.videoRepo.Get(ctx, videoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	// The signature must be for this video's folder, not some other video the caller may watch.
	if video.Status != "ready" || video.HLSPrefix == "" || signedPrefix != "/"+video.HLSPrefix {
		return nil, ErrBadStreamSignature
	}

	data, err := s.store.ReadSmallObject(ctx, video.HLSPrefix+fileName, maxPlaylistBytes)
	if errors.Is(err, storage.ErrObjectNotFound) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}

	signURI := func(uri string) string {
		if strings.Contains(uri, "://") || strings.HasPrefix(uri, "/") || strings.Contains(uri, "..") {
			return uri // never sign anything outside the video's folder
		}
		if playlistNamePattern.MatchString(uri) {
			return playlistBase + uri + "?" + query
		}
		return s.signer.URL(video.HLSPrefix+uri) + "?" + query
	}
	return rewritePlaylist(data, signURI), nil
}

// uriAttrPattern finds URI="..." inside tags such as #EXT-X-MEDIA and #EXT-X-MAP.
var uriAttrPattern = regexp.MustCompile(`URI="([^"]*)"`)

// rewritePlaylist passes every URI line and URI="..." attribute of an .m3u8 file through signURI.
func rewritePlaylist(data []byte, signURI func(string) string) []byte {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
		case strings.HasPrefix(line, "#"):
			lines[i] = uriAttrPattern.ReplaceAllStringFunc(line, func(m string) string {
				return `URI="` + signURI(uriAttrPattern.FindStringSubmatch(m)[1]) + `"`
			})
		default:
			lines[i] = signURI(line)
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

// ListLessonVideos lists a lesson's videos. Owners see every status; students only ready videos of published lessons.
func (s *VideoService) ListLessonVideos(ctx context.Context, user *models.User, lessonID string) ([]models.Video, error) {
	if !uuidPattern.MatchString(lessonID) {
		return nil, ErrLessonNotFound
	}
	lesson, err := s.lessonRepo.GetLesson(ctx, lessonID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	isTeacher, freeOnly, err := s.videoAccess(ctx, user, lesson)
	if err != nil {
		return nil, notFoundAs(err, ErrLessonNotFound)
	}
	return s.videoRepo.List(ctx, models.VideoListFilter{
		CourseID: lesson.CourseID, LessonID: lesson.ID, OnlyReady: !isTeacher, OnlyFree: freeOnly,
	})
}

// ListCourseVideos lists every video of a course the user may see, in lesson order.
func (s *VideoService) ListCourseVideos(ctx context.Context, user *models.User, courseID string) ([]models.Video, error) {
	isTeacher, freeOnly, err := s.courseAccess(ctx, user, courseID)
	if err != nil {
		return nil, notFoundAs(err, ErrCourseNotFound)
	}
	return s.videoRepo.List(ctx, models.VideoListFilter{
		CourseID: courseID, OnlyReady: !isTeacher, OnlyFree: freeOnly,
	})
}

// courseAccess is authorizeCourse with a fallback: users who may not read the whole course can still
// see its free content when the course is published (freeOnly).
func (s *VideoService) courseAccess(ctx context.Context, user *models.User, courseID string) (isTeacher, freeOnly bool, err error) {
	isTeacher, err = authorizeCourse(ctx, s.lessonRepo, user, courseID)
	if err == nil {
		return isTeacher, false, nil
	}
	if !errors.Is(err, ErrForbidden) {
		return false, false, err
	}
	published, perr := s.lessonRepo.IsCoursePublished(ctx, courseID)
	if perr != nil {
		return false, false, perr
	}
	if !published {
		return false, false, ErrCourseNotFound
	}
	return false, true, nil
}

// videoAccess decides how a user may see a lesson's videos. Non-owners never see unpublished lessons.
func (s *VideoService) videoAccess(ctx context.Context, user *models.User, lesson *models.Lesson) (isTeacher, freeOnly bool, err error) {
	isTeacher, freeOnly, err = s.courseAccess(ctx, user, lesson.CourseID)
	if err != nil {
		return false, false, err
	}
	if !isTeacher && !lesson.IsPublished {
		return false, false, ErrLessonNotFound
	}
	return isTeacher, freeOnly, nil
}

// notFoundAs maps "may not see it" errors to the caller's not-found error.
func notFoundAs(err, notFound error) error {
	if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) || errors.Is(err, ErrLessonNotFound) {
		return notFound
	}
	return err
}

// ownedVideo loads a video the user uploaded to a course they own. Anything else looks like a missing video.
func (s *VideoService) ownedVideo(ctx context.Context, user *models.User, videoID string) (*models.Video, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if !uuidPattern.MatchString(videoID) {
		return nil, ErrVideoNotFound
	}
	video, err := s.videoRepo.Get(ctx, videoID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	if video.UploadedBy != user.ID || video.CourseID == "" {
		return nil, ErrVideoNotFound
	}
	if err := requireTeacher(ctx, s.lessonRepo, user, video.CourseID); err != nil {
		if errors.Is(err, ErrForbidden) || errors.Is(err, ErrCourseNotFound) {
			return nil, ErrVideoNotFound
		}
		return nil, err
	}
	return video, nil
}

// validateVideoFileName cleans a client-supplied file name and returns it with its extension (".mp4")
// and the content type the upload must use. The name is only a display title, never part of the object key.
func validateVideoFileName(fileName string) (name, ext, contentType string, err error) {
	name = strings.ReplaceAll(fileName, "\\", "/")
	name = name[strings.LastIndex(name, "/")+1:]
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name))

	n := utf8.RuneCountInString(name)
	if n < 1 || n > maxVideoFileNameChars {
		return "", "", "", fmt.Errorf("%w: fileName must be 1 to %d characters", ErrInvalidInput, maxVideoFileNameChars)
	}
	ext = strings.ToLower(path.Ext(name))
	contentType, ok := storage.VideoContentType(ext)
	if !ok {
		return "", "", "", fmt.Errorf("%w: video must be .mp4, .mov, .mkv or .webm", ErrInvalidInput)
	}
	return name, ext, contentType, nil
}
