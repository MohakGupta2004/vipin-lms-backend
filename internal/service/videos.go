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
	"strconv"
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
	maxVideoTitleChars    = 255
	// maxVideoDescriptionChars caps a lecture description, a few paragraphs of plain text.
	maxVideoDescriptionChars = 5000
	// streamURLExpiry is short so a leaked URL stops working soon; clients refetch before expiresAt.
	streamURLExpiry = 15 * time.Minute
	// maxPlaylistBytes caps an .m3u8 file read from storage; real ones are a few KB.
	maxPlaylistBytes = 1 << 20
	// maxVideoPositionSec caps a saved playback position (24 hours), since video durations are not stored yet.
	maxVideoPositionSec = 24 * 60 * 60
	// videoCleanupTimeout bounds removing a deleted video's files: one source file plus every HLS segment.
	videoCleanupTimeout = 2 * time.Minute
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
	// ErrVideoProcessing means the video is being transcoded and can't be deleted until that ends.
	ErrVideoProcessing = errors.New("video is still processing; delete it once it is ready or has failed")
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

// UpdateVideoInput holds the video fields to change. Nil fields are left as they are.
type UpdateVideoInput struct {
	Title       *string
	Description *string
	Kind        *string  // offline or meet: moves the video to the other section
	Tiers       []string // nil = unchanged
	IsFree      *bool    // deprecated: true adds the free tier, false removes it
}

// validateVideoKind accepts "" (meaning the default where one applies), offline and meet.
func validateVideoKind(kind string) error {
	switch kind {
	case "", models.VideoKindOffline, models.VideoKindMeet:
		return nil
	}
	return fmt.Errorf("%w: kind must be %s or %s", ErrInvalidInput, models.VideoKindOffline, models.VideoKindMeet)
}

func validateVideoDescription(description string) error {
	if utf8.RuneCountInString(description) > maxVideoDescriptionChars {
		return fmt.Errorf("%w: description must be at most %d characters", ErrInvalidInput, maxVideoDescriptionChars)
	}
	return nil
}

// RequestUpload lets the course owner start a video upload on a lesson of their course.
// It creates the video row ('uploading') and returns a signed PUT URL straight to the bucket.
// kind is offline (the default) or meet; tiers lists who can watch it (nil = the kind's default).
func (s *VideoService) RequestUpload(ctx context.Context, user *models.User, lessonID, fileName, description, kind string, tiers []string) (*VideoUpload, error) {
	if !user.CanTeach() {
		return nil, ErrForbidden
	}
	if err := validateVideoKind(kind); err != nil {
		return nil, err
	}
	if kind == "" {
		kind = models.VideoKindOffline
	}
	tiers, err := normalizeTiers(tiers, defaultVideoTiers(kind))
	if err != nil {
		return nil, err
	}
	description = strings.TrimSpace(description)
	if err := validateVideoDescription(description); err != nil {
		return nil, err
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
		Description: description,
		Kind:        kind,
		Tiers:       tiers,
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

// UpdateVideo lets the person who uploaded a video change its title, description, section (kind) and tiers.
func (s *VideoService) UpdateVideo(ctx context.Context, user *models.User, videoID string, in UpdateVideoInput) (*models.Video, error) {
	video, err := s.ownedVideo(ctx, user, videoID)
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		video.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		video.Description = strings.TrimSpace(*in.Description)
	}
	if in.Kind != nil {
		if err := validateVideoKind(*in.Kind); err != nil || *in.Kind == "" {
			return nil, fmt.Errorf("%w: kind must be %s or %s", ErrInvalidInput, models.VideoKindOffline, models.VideoKindMeet)
		}
		video.Kind = *in.Kind
	}
	switch {
	case in.Tiers != nil:
		if video.Tiers, err = normalizeTiers(in.Tiers, nil); err != nil {
			return nil, err
		}
	case in.IsFree != nil:
		video.Tiers = legacyFreeTiers(video.Tiers, *in.IsFree, defaultVideoTiers(video.Kind))
	}
	if n := utf8.RuneCountInString(video.Title); n < 1 || n > maxVideoTitleChars {
		return nil, fmt.Errorf("%w: title must be 1 to %d characters", ErrInvalidInput, maxVideoTitleChars)
	}
	if err := validateVideoDescription(video.Description); err != nil {
		return nil, err
	}
	err = s.videoRepo.UpdateDetails(ctx, video.ID, video.Title, video.Description, video.Kind, video.Tiers)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrVideoNotFound
	}
	if err != nil {
		return nil, err
	}
	return video, nil
}

// DeleteVideo lets the person who uploaded a video (and still owns its course) delete it: the row goes at once,
// then the source file and HLS folder are removed from the bucket in the background. A video that is still
// transcoding can't be deleted, so the job never writes into a deleted folder.
func (s *VideoService) DeleteVideo(ctx context.Context, user *models.User, videoID string) error {
	video, err := s.ownedVideo(ctx, user, videoID)
	if err != nil {
		return err
	}
	// Deleting the row while storage is off would leave the files behind forever.
	if !s.store.Enabled() {
		return ErrStorageDisabled
	}
	busy, err := s.videoRepo.Delete(ctx, video.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrVideoNotFound
	}
	if err != nil {
		return err
	}
	if busy {
		return ErrVideoProcessing
	}
	// Everything of a video lives under its own folder: courses/{course}/videos/{video}/ (source + hls/).
	go deleteVideoFiles(s.store, path.Dir(video.OriginalKey)+"/")
	return nil
}

// deleteVideoFiles removes a deleted video's folder on a best-effort basis; a failure only leaves orphan files.
// It uses a fresh context because the request is over by the time it runs.
func deleteVideoFiles(store storage.VideoStore, prefix string) {
	ctx, cancel := context.WithTimeout(context.Background(), videoCleanupTimeout)
	defer cancel()
	if err := store.DeletePrefix(ctx, prefix); err != nil {
		slog.Error("failed to delete stored video files", "prefix", prefix, "err", err)
	}
}

// GetVideo returns a video to the person who uploaded it, if they still own its course.
func (s *VideoService) GetVideo(ctx context.Context, user *models.User, videoID string) (*models.Video, error) {
	return s.ownedVideo(ctx, user, videoID)
}

// VideoStream is a ready-to-use signed manifest URL. The manifest is served by the API with every
// playlist and segment URL inside already signed, so any HLS player can open it as-is.
// QueryParams is the bare signature, for clients that want to build URLs themselves.
// ResumeAt is where the user stopped last time, in seconds (0 = from the start).
type VideoStream struct {
	ManifestURL string
	QueryParams string
	ExpiresAt   time.Time
	ResumeAt    int
}

// StreamVideo returns a short-lived signed URL for a ready video the user may watch.
// playlistBase is the API URL that serves playlists, e.g. "http://localhost:3000/api/v1/videos/{id}/hls/".
// When the user has a saved position, the manifest URL carries it as "start" so the playlist tells
// the player to begin there.
func (s *VideoService) StreamVideo(ctx context.Context, user *models.User, videoID, playlistBase string) (*VideoStream, error) {
	if s.signer == nil {
		return nil, ErrStorageDisabled
	}
	video, err := s.watchableVideo(ctx, user, videoID)
	if err != nil {
		return nil, err
	}
	resumeAt, err := s.videoRepo.GetProgress(ctx, user.ID, video.ID)
	if err != nil {
		return nil, err
	}

	expires := time.Now().Add(streamURLExpiry)
	query := s.signer.SignPrefix("/"+video.HLSPrefix, expires)
	manifestURL := playlistBase + "manifest.m3u8?" + query
	if resumeAt > 0 {
		manifestURL += "&start=" + strconv.Itoa(resumeAt)
	}
	return &VideoStream{
		ManifestURL: manifestURL,
		QueryParams: query,
		ExpiresAt:   expires,
		ResumeAt:    resumeAt,
	}, nil
}

// completeAtFraction is how much of a video counts as watching it: past 90% it is marked completed,
// since players usually stop on the credits or a few seconds short of the end.
const completeAtFraction = 0.9

// SaveProgress stores where the user stopped in a video they may watch, so the next stream resumes there.
// durationSec, when the player knows it, is stored with it, and reaching 90% of it marks the video completed.
func (s *VideoService) SaveProgress(ctx context.Context, user *models.User, videoID string, positionSec int, durationSec *int) error {
	if positionSec < 0 || positionSec > maxVideoPositionSec {
		return fmt.Errorf("%w: positionSec must be 0 to %d", ErrInvalidInput, maxVideoPositionSec)
	}
	if durationSec != nil && (*durationSec < 1 || *durationSec > maxVideoPositionSec) {
		return fmt.Errorf("%w: durationSec must be 1 to %d", ErrInvalidInput, maxVideoPositionSec)
	}
	video, err := s.watchableVideo(ctx, user, videoID)
	if err != nil {
		return err
	}
	reached := durationSec != nil && float64(positionSec) >= completeAtFraction*float64(*durationSec)
	return s.videoRepo.SaveProgress(ctx, user.ID, video.ID, positionSec, durationSec, reached)
}

// SetCompleted lets a user mark a video they may watch as completed, or not completed.
func (s *VideoService) SetCompleted(ctx context.Context, user *models.User, videoID string, completed bool) error {
	video, err := s.watchableVideo(ctx, user, videoID)
	if err != nil {
		return err
	}
	return s.videoRepo.SetCompleted(ctx, user.ID, video.ID, completed)
}

// watchableVideo loads a ready video the user may watch: the course owner, an enrolled student, or any
// logged-in user for a free video in a published course. Anything else looks like a missing video.
func (s *VideoService) watchableVideo(ctx context.Context, user *models.User, videoID string) (*models.Video, error) {
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
	isTeacher, tier, err := s.videoAccess(ctx, user, lesson)
	if err != nil {
		return nil, notFoundAs(err, ErrVideoNotFound)
	}
	if videoLocked(video, tier) {
		return nil, ErrVideoNotFound
	}
	if video.Status != "ready" || video.HLSPrefix == "" {
		if !isTeacher {
			return nil, ErrVideoNotFound
		}
		return nil, ErrVideoNotReady
	}
	return video, nil
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
	data = rewritePlaylist(data, signURI)
	// "start" is not signed: changing it only moves where this viewer's own playback begins.
	if start, err := strconv.Atoi(q.Get("start")); err == nil && fileName == "manifest.m3u8" && start > 0 && start <= maxVideoPositionSec {
		data = addStartTime(data, start)
	}
	return data, nil
}

// addStartTime tells HLS players to begin playback at sec, by adding #EXT-X-START after the #EXTM3U header.
func addStartTime(data []byte, sec int) []byte {
	header, rest, found := strings.Cut(string(data), "\n")
	if !found || header != "#EXTM3U" {
		return data
	}
	return []byte(header + "\n#EXT-X-START:TIME-OFFSET=" + strconv.Itoa(sec) + ",PRECISE=YES\n" + rest)
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

// ListLessonVideos lists a lesson's videos, optionally of one kind (offline or meet). Owners see every status;
// students only ready videos of published lessons. Videos the user's tier cannot stream have locked set.
func (s *VideoService) ListLessonVideos(ctx context.Context, user *models.User, lessonID, kind string) ([]models.Video, error) {
	if err := validateVideoKind(kind); err != nil {
		return nil, err
	}
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
	isTeacher, tier, err := s.videoAccess(ctx, user, lesson)
	if err != nil {
		return nil, notFoundAs(err, ErrLessonNotFound)
	}
	videos, err := s.videoRepo.List(ctx, models.VideoListFilter{
		CourseID: lesson.CourseID, LessonID: lesson.ID, OnlyReady: !isTeacher, Kind: kind,
	})
	if err != nil {
		return nil, err
	}
	for i := range videos {
		videos[i].Locked = videoLocked(&videos[i], tier)
	}
	return videos, nil
}

// ListCourseVideos lists every video of a course the user may see, in lesson order, each with the
// user's own progress. Users previewing the course get every ready video so they can see the whole
// curriculum, with locked set on the ones their tier cannot stream. kind keeps one kind (offline or meet).
func (s *VideoService) ListCourseVideos(ctx context.Context, user *models.User, courseID, kind string) ([]models.Video, error) {
	if err := validateVideoKind(kind); err != nil {
		return nil, err
	}
	isTeacher, tier, err := s.courseAccess(ctx, user, courseID)
	if err != nil {
		return nil, notFoundAs(err, ErrCourseNotFound)
	}
	videos, err := s.videoRepo.List(ctx, models.VideoListFilter{CourseID: courseID, OnlyReady: !isTeacher, Kind: kind})
	if err != nil {
		return nil, err
	}
	progress, err := s.videoRepo.ListProgress(ctx, user.ID, courseID)
	if err != nil {
		return nil, err
	}
	for i := range videos {
		videos[i].Progress = progress[videos[i].ID]
		videos[i].Locked = videoLocked(&videos[i], tier)
	}
	return videos, nil
}

// videoLocked reports whether a user on this tier is shut out of a video: every video not tagged for
// the tier or free. The lesson's own free flag does not unlock its videos.
func videoLocked(v *models.Video, tier models.Tier) bool {
	return !models.CanAccess(tier, v.Tiers)
}

// courseAccess tells the tier a user has in a course, whose videos they may then list or stream.
func (s *VideoService) courseAccess(ctx context.Context, user *models.User, courseID string) (isTeacher bool, tier models.Tier, err error) {
	isTeacher, tier, err = authorizeTier(ctx, s.lessonRepo, user, courseID)
	if errors.Is(err, ErrForbidden) {
		return false, "", ErrCourseNotFound
	}
	return isTeacher, tier, err
}

// videoAccess tells the tier a user has for a lesson's videos. Non-owners never see unpublished lessons.
func (s *VideoService) videoAccess(ctx context.Context, user *models.User, lesson *models.Lesson) (isTeacher bool, tier models.Tier, err error) {
	isTeacher, tier, err = s.courseAccess(ctx, user, lesson.CourseID)
	if err != nil {
		return false, "", err
	}
	if !isTeacher && !lesson.IsPublished {
		return false, "", ErrLessonNotFound
	}
	return isTeacher, tier, nil
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
