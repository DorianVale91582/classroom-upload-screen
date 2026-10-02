package screening

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type Submission struct {
	CourseID  string
	LearnerID string
	Deadline  time.Time
	Caption   string
	Filename  string
	MediaType string
	Image     []byte
	RequestID string
}

type Decision struct {
	State      string          `json:"state"`
	CourseID   string          `json:"course_id"`
	LearnerID  string          `json:"learner_id"`
	Deadline   time.Time       `json:"deadline"`
	OnTime     bool            `json:"on_time"`
	Transform  json.RawMessage `json:"transform,omitempty"`
	ReviewedAt time.Time       `json:"reviewed_at"`
}

type Moderator interface {
	Flagged(context.Context, Submission) (bool, error)
}

type Transformer interface {
	Resize(context.Context, Submission) (json.RawMessage, error)
}

type Workflow struct {
	moderator   Moderator
	transformer Transformer
	now         func() time.Time
}

func New(m Moderator, t Transformer, now func() time.Time) *Workflow {
	return &Workflow{moderator: m, transformer: t, now: now}
}

func (w *Workflow) Screen(ctx context.Context, in Submission) (Decision, error) {
	if in.CourseID == "" || in.LearnerID == "" || in.Caption == "" || len(in.Image) == 0 || in.RequestID == "" || in.Deadline.IsZero() {
		return Decision{}, fmt.Errorf("course_id, learner_id, deadline, caption, image, and request_id are required")
	}
	now := w.now().UTC()
	result := Decision{CourseID: in.CourseID, LearnerID: in.LearnerID, Deadline: in.Deadline, OnTime: !now.After(in.Deadline), ReviewedAt: now}
	flagged, err := w.moderator.Flagged(ctx, in)
	if err != nil {
		return Decision{}, fmt.Errorf("moderate submission: %w", err)
	}
	if flagged {
		result.State = "quarantine"
		return result, nil
	}
	transform, err := w.transformer.Resize(ctx, in)
	if err != nil {
		return Decision{}, fmt.Errorf("resize approved image: %w", err)
	}
	result.State = "publish"
	result.Transform = transform
	return result, nil
}
