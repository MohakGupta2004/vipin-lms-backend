package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path"
	"sync"
	"time"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/storage"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/transcoder"
)

const (
	transcodeWorkers      = 5
	transcodeQueueSize    = 256
	transcodePollInterval = 15 * time.Second
	transcodeTimeout      = 3 * time.Hour
)

// VideoTranscoder starts and inspects transcoding jobs. Implemented by *transcoder.Client.
type VideoTranscoder interface {
	StartJob(ctx context.Context, inputURI, outputURI string) (jobName string, err error)
	JobStatus(ctx context.Context, jobName string) (state, errMsg string, err error)
}

var _ VideoTranscoder = (*transcoder.Client)(nil)

// TranscodeQueue runs video transcoding jobs on a small in-process worker pool.
// Videos still 'processing' at boot are re-queued; their stored job name is polled again, so no duplicate job starts.
type TranscodeQueue struct {
	jobs       chan string
	videoRepo  *models.VideoRepository
	store      storage.VideoStore
	transcoder VideoTranscoder
	wg         sync.WaitGroup
}

var _ TranscodeEnqueuer = (*TranscodeQueue)(nil)

func NewTranscodeQueue(videoRepo *models.VideoRepository, store storage.VideoStore, tc VideoTranscoder) *TranscodeQueue {
	return &TranscodeQueue{
		jobs:       make(chan string, transcodeQueueSize),
		videoRepo:  videoRepo,
		store:      store,
		transcoder: tc,
	}
}

// Start launches the workers and re-queues videos left 'processing' by a previous run.
// Workers stop when ctx is cancelled; call Wait to let them finish.
func (q *TranscodeQueue) Start(ctx context.Context) {
	for i := 0; i < transcodeWorkers; i++ {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-q.jobs:
					q.process(ctx, id)
				}
			}
		}()
	}

	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		ids, err := q.videoRepo.ListProcessingIDs(ctx)
		if err != nil {
			slog.Error("list processing videos failed", "err", err)
			return
		}
		for _, id := range ids {
			if err := q.Enqueue(ctx, id); err != nil {
				return
			}
		}
	}()
}

// Enqueue queues a video for transcoding. It blocks while the queue is full, until ctx is done.
func (q *TranscodeQueue) Enqueue(ctx context.Context, videoID string) error {
	select {
	case q.jobs <- videoID:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wait blocks until all workers have stopped. Cancel the Start context first.
func (q *TranscodeQueue) Wait() { q.wg.Wait() }

// process drives one video to ready or failed. On ctx cancel it returns and leaves the video 'processing'.
func (q *TranscodeQueue) process(ctx context.Context, id string) {
	log := slog.With("video", id)

	video, err := q.videoRepo.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			log.Error("load video for transcoding failed", "err", err)
		}
		return
	}
	if video.Status != "processing" {
		return
	}

	dir := path.Dir(video.OriginalKey)
	hlsPrefix := dir + "/hls/"

	job := video.TranscodeJob
	if job == "" {
		job, err = q.transcoder.StartJob(ctx, q.store.URI(video.OriginalKey), q.store.URI(hlsPrefix))
		if err != nil {
			if ctx.Err() == nil {
				log.Error("start transcode job failed", "err", err)
				q.fail(id, "could not start transcoding")
			}
			return
		}
		if err := q.videoRepo.SetTranscodeJob(ctx, id, job); err != nil {
			if ctx.Err() == nil {
				log.Error("save transcode job failed", "job", job, "err", err)
				q.fail(id, "could not start transcoding")
			}
			return
		}
	}

	deadline := time.NewTimer(transcodeTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(transcodePollInterval)
	defer ticker.Stop()

	for {
		state, errMsg, err := q.transcoder.JobStatus(ctx, job)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			log.Warn("poll transcode job failed", "job", job, "err", err)
		case state == transcoder.StateSucceeded:
			if err := q.videoRepo.MarkReady(ctx, id, hlsPrefix); err != nil && ctx.Err() == nil {
				log.Error("mark video ready failed", "err", err)
			}
			return
		case state == transcoder.StateFailed:
			log.Error("transcode job failed", "job", job, "msg", errMsg)
			q.fail(id, errMsg)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			q.fail(id, "transcoding timed out")
			return
		case <-ticker.C:
		}
	}
}

// fail marks a video failed. It ignores the worker context so a failure is still recorded during shutdown.
func (q *TranscodeQueue) fail(id, msg string) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	if err := q.videoRepo.MarkFailed(ctx, id, msg); err != nil {
		slog.Error("mark video failed failed", "video", id, "err", err)
	}
}
