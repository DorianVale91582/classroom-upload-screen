package screening

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
	"time"
)

type fakeModerator struct{ flagged bool }

func (f fakeModerator) Flagged(context.Context, Submission) (bool, error) { return f.flagged, nil }

type fakeTransformer struct{ calls int }

func (f *fakeTransformer) Resize(context.Context, Submission) (json.RawMessage, error) {
	f.calls++
	return json.RawMessage(`{"asset":"lesson.webp"}`), nil
}

func TestWorkflowDecision(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	deadline := now.Add(time.Hour)
	tests := []struct {
		name           string
		flagged        bool
		wantState      string
		wantTransforms int
	}{
		{name: "safe image and caption publish", wantState: "publish", wantTransforms: 1},
		{name: "flagged image or caption quarantines", flagged: true, wantState: "quarantine", wantTransforms: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := &fakeTransformer{}
			workflow := New(fakeModerator{flagged: tt.flagged}, transformer, func() time.Time { return now })
			got, err := workflow.Screen(context.Background(), Submission{
				CourseID: "physics-101", LearnerID: "learner-7", Deadline: deadline,
				Caption: "My pendulum lab", Filename: "lab.jpg", MediaType: "image/jpeg", Image: []byte("image"), RequestID: "submission-42",
			})
			if err != nil {
				t.Fatal(err)
			}
			if got.State != tt.wantState || transformer.calls != tt.wantTransforms {
				t.Fatalf("state=%q transforms=%d, want state=%q transforms=%d", got.State, transformer.calls, tt.wantState, tt.wantTransforms)
			}
			if !got.OnTime {
				t.Fatal("submission should be reported on time")
			}
		})
	}
}

func TestResizeBodyIncludesIdempotencyKey(t *testing.T) {
	body, contentType, err := resizeBody(Submission{
		Filename: "lab.jpg", MediaType: "image/jpeg", Image: []byte("image"), RequestID: "submission-42",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(body, params["boundary"])
	fields := make(map[string]string)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		value, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		if part.FormName() != "image" {
			fields[part.FormName()] = string(value)
		}
	}
	if got := fields["idempotency_key"]; got != "submission-42" {
		t.Fatalf("idempotency_key=%q, want submission-42", got)
	}
	for name := range fields {
		if !strings.Contains(" width height fit enlarge format idempotency_key store ", " "+name+" ") {
			t.Fatalf("unsupported resize field %q", name)
		}
	}
}
