// Package transcoder wraps the Google Cloud Transcoder API.
package transcoder

import (
	"context"
	"errors"
	"fmt"
	"strings"

	transcoder "cloud.google.com/go/video/transcoder/apiv1"
	"cloud.google.com/go/video/transcoder/apiv1/transcoderpb"
)

// Job states reported by JobStatus.
const (
	StatePending   = "PENDING"
	StateRunning   = "RUNNING"
	StateSucceeded = "SUCCEEDED"
	StateFailed    = "FAILED"
)

// Client starts and inspects transcoding jobs. Credentials come from Application Default Credentials.
type Client struct {
	svc    *transcoder.Client
	parent string
}

func New(ctx context.Context, projectID, location string) (*Client, error) {
	if projectID == "" || location == "" {
		return nil, errors.New("transcoder: project id and location are required")
	}
	svc, err := transcoder.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("transcoder: create client: %w", err)
	}
	return &Client{svc: svc, parent: fmt.Sprintf("projects/%s/locations/%s", projectID, location)}, nil
}

func (c *Client) Close() error { return c.svc.Close() }

// StartJob creates a job that turns inputURI into HLS and DASH under outputURI (a gs:// folder ending in "/").
// It returns the full job name, to be passed to JobStatus.
func (c *Client) StartJob(ctx context.Context, inputURI, outputURI string) (string, error) {
	if !strings.HasSuffix(outputURI, "/") {
		outputURI += "/"
	}
	job, err := c.svc.CreateJob(ctx, &transcoderpb.CreateJobRequest{
		Parent: c.parent,
		Job: &transcoderpb.Job{
			InputUri:  inputURI,
			OutputUri: outputURI,
			JobConfig: &transcoderpb.Job_TemplateId{TemplateId: "preset/web-hd"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("transcoder: create job: %w", err)
	}
	return job.GetName(), nil
}

// JobStatus returns the job's state (one of the State constants) and, when it failed, an error message.
func (c *Client) JobStatus(ctx context.Context, jobName string) (state, errMsg string, err error) {
	job, err := c.svc.GetJob(ctx, &transcoderpb.GetJobRequest{Name: jobName})
	if err != nil {
		return "", "", fmt.Errorf("transcoder: get job: %w", err)
	}
	switch job.GetState() {
	case transcoderpb.Job_SUCCEEDED:
		return StateSucceeded, "", nil
	case transcoderpb.Job_FAILED:
		msg := "transcoding failed"
		if e := job.GetError(); e != nil && e.GetMessage() != "" {
			msg = e.GetMessage()
		}
		return StateFailed, msg, nil
	case transcoderpb.Job_RUNNING:
		return StateRunning, "", nil
	default:
		return StatePending, "", nil
	}
}
