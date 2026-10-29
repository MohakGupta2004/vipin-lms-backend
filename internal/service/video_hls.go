package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
)

const (
	// hlsManifestName is the master playlist every video's hls/ folder starts from.
	hlsManifestName = "manifest.m3u8"
	// maxHLSFiles caps the files of one video: four playlists of 6-second segments for 5 hours is about 12,000.
	maxHLSFiles = 20_000
	// maxHLSBytes caps one video's HLS files, about 5 hours of a 1080p/720p/480p ladder.
	maxHLSBytes int64 = 25 << 30
	// abandonedUploadAfter is how long an 'uploading' video may go without renewing its upload form before
	// its row and files are removed. Browsers renew at least hourly while they work, even after time offline.
	abandonedUploadAfter = 24 * time.Hour
	// legacyTranscodeTimeout is how long the old server-side transcoder could take before a video counted as failed.
	legacyTranscodeTimeout = 3 * time.Hour
	// maintenanceInterval is how often abandoned uploads are looked for.
	maintenanceInterval = time.Hour
	// maintenanceBatch caps the videos handled in one pass, so a backlog is worked off over several passes.
	maintenanceBatch = 100
)

// mediaPathPattern is an init or media segment a media playlist may reference: one folder down, e.g. "720p/seg-00001.m4s".
var mediaPathPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+/[A-Za-z0-9_-]+\.(m4s|mp4)$`)

// browserTranscoded reports whether the instructor's browser made the video's HLS files (see Video.OriginalKey).
func browserTranscoded(video *models.Video) bool {
	return strings.HasSuffix(video.OriginalKey, "/hls/"+hlsManifestName)
}

// hlsFolder is the folder a browser-transcoded video's HLS files are uploaded to: courses/{c}/videos/{v}/hls/.
func hlsFolder(video *models.Video) string {
	return path.Dir(video.OriginalKey) + "/"
}

// uploadedHLS is the outcome of checking a video's uploaded HLS folder.
type uploadedHLS struct {
	size  int64    // bytes of every file the playlists use
	stray []string // object names in the folder that no playlist uses
}

// checkUploadedHLS checks the HLS files a browser uploaded under prefix before they are served to students.
// It costs one listing (a few pages) plus one small read per playlist.
func checkUploadedHLS(ctx context.Context, store storage.VideoStore, prefix string) (*uploadedHLS, error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrInvalidInput}, args...)...)
	}

	objects, err := store.ListObjects(ctx, prefix, maxHLSFiles)
	if errors.Is(err, storage.ErrTooManyObjects) {
		return nil, invalid("the video has more than %d files", maxHLSFiles)
	}
	if err != nil {
		return nil, err
	}
	stored := make(map[string]storage.ObjectInfo, len(objects))
	for _, o := range objects {
		stored[strings.TrimPrefix(o.Name, prefix)] = o
	}
	if _, ok := stored[hlsManifestName]; !ok {
		return nil, invalid("video files not uploaded yet")
	}

	used := map[string]bool{}
	// use marks a file as part of the video after checking it exists with the content type its extension needs.
	use := func(name string) error {
		o, ok := stored[name]
		if !ok {
			return invalid("uploaded video is missing %s", name)
		}
		if want, _ := storage.HLSContentType(path.Ext(name)); o.ContentType != want {
			return invalid("%s was uploaded with the wrong content type", name)
		}
		used[name] = true
		return nil
	}
	readPlaylist := func(name string) ([]byte, error) {
		if err := use(name); err != nil {
			return nil, err
		}
		data, err := store.ReadSmallObject(ctx, prefix+name, maxPlaylistBytes)
		if errors.Is(err, storage.ErrObjectNotFound) {
			return nil, invalid("uploaded video is missing %s", name)
		}
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(string(data), "#EXTM3U") {
			return nil, invalid("%s is not an HLS playlist", name)
		}
		return data, nil
	}

	manifest, err := readPlaylist(hlsManifestName)
	if err != nil {
		return nil, err
	}
	variants := playlistURIs(manifest)
	if len(variants) == 0 {
		return nil, invalid("%s lists no renditions", hlsManifestName)
	}
	for _, name := range variants {
		if !playlistNamePattern.MatchString(name) || name == hlsManifestName {
			return nil, invalid("%s points at an unexpected file", hlsManifestName)
		}
		if used[name] {
			continue // the shared audio playlist is listed once per video rendition
		}
		data, err := readPlaylist(name)
		if err != nil {
			return nil, err
		}
		// Without the end tag players treat the video as a live stream that is still growing.
		if !strings.Contains(string(data), "#EXT-X-ENDLIST") {
			return nil, invalid("%s is incomplete", name)
		}
		segments := 0
		for _, media := range playlistURIs(data) {
			if !mediaPathPattern.MatchString(media) {
				return nil, invalid("%s points at an unexpected file", name)
			}
			if err := use(media); err != nil {
				return nil, err
			}
			if path.Ext(media) == ".m4s" {
				segments++
			}
		}
		if segments == 0 {
			return nil, invalid("%s has no segments", name)
		}
	}

	out := &uploadedHLS{}
	for name, o := range stored {
		if used[name] {
			out.size += o.Size
		} else {
			out.stray = append(out.stray, o.Name)
		}
	}
	if out.size > maxHLSBytes {
		return nil, invalid("the video is larger than %d GB", maxHLSBytes>>30)
	}
	return out, nil
}

// playlistURIs returns every URI in an .m3u8 file, read exactly the way VideoPlaylist rewrites them.
func playlistURIs(data []byte) []string {
	var uris []string
	rewritePlaylist(data, func(uri string) string {
		uris = append(uris, uri)
		return uri
	})
	return uris
}

// deleteStrayFiles removes uploaded files no playlist uses (left from an earlier attempt), best effort.
func deleteStrayFiles(store storage.VideoStore, objects []string) {
	ctx, cancel := context.WithTimeout(context.Background(), videoCleanupTimeout)
	defer cancel()
	for _, object := range objects {
		if err := store.DeleteObject(ctx, object); err != nil {
			slog.Error("failed to delete stray video file", "object", object, "err", err)
			return
		}
	}
}

// RunMaintenance removes abandoned browser uploads, empties the bucket folders of deleted videos and settles
// videos the old server-side transcoder left 'processing', once at start and then every hour, until ctx is
// cancelled.
func (s *VideoService) RunMaintenance(ctx context.Context) {
	if !s.store.Enabled() {
		return
	}
	ticker := time.NewTicker(maintenanceInterval)
	defer ticker.Stop()
	for {
		s.settleLegacyTranscodes(ctx)
		s.sweepAbandonedUploads(ctx)
		s.removeDeletedFiles(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sweepAbandonedUploads deletes videos whose browser stopped uploading (tab closed, laptop gone) and queues
// whatever files they had uploaded for removal, so partial uploads don't pile up in the bucket.
func (s *VideoService) sweepAbandonedUploads(ctx context.Context) {
	videos, err := s.videoRepo.ListStaleUploads(ctx, abandonedUploadAfter, maintenanceBatch)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("list abandoned video uploads failed", "err", err)
		}
		return
	}
	for i := range videos {
		v := &videos[i]
		// Re-checks the age, so a browser that renewed its form a moment ago keeps its video.
		err := s.videoRepo.DeleteStaleUpload(ctx, v.ID, abandonedUploadAfter)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("delete abandoned video upload failed", "video", v.ID, "err", err)
			}
			return
		}
		slog.Info("removed abandoned video upload", "video", v.ID)
	}
}

// settleLegacyTranscodes finishes videos the old server-side transcoder was working on: ready when its output
// is in the bucket, failed once it has had longer than any job took. There is no transcoder to ask any more.
func (s *VideoService) settleLegacyTranscodes(ctx context.Context) {
	videos, err := s.videoRepo.ListProcessing(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("list processing videos failed", "err", err)
		}
		return
	}
	for i := range videos {
		v := &videos[i]
		hlsPrefix := path.Dir(v.OriginalKey) + "/hls/"
		_, _, err := s.store.ObjectInfo(ctx, hlsPrefix+hlsManifestName)
		switch {
		case err == nil:
			err = s.videoRepo.MarkReady(ctx, v.ID, hlsPrefix)
		case errors.Is(err, storage.ErrObjectNotFound) && time.Since(v.UpdatedAt) > legacyTranscodeTimeout:
			err = s.videoRepo.MarkFailed(ctx, v.ID, "Transcoding did not finish. Delete this video and upload it again.")
		case errors.Is(err, storage.ErrObjectNotFound):
			err = nil // its job may still be writing; look again next pass
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			if ctx.Err() == nil {
				slog.Error("settle processing video failed", "video", v.ID, "err", err)
			}
			return
		}
	}
}
