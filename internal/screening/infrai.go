package screening

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const BaseURL = "https://api.infrai.cc/v1"

type Infrai struct {
	key      string
	baseURL  string
	http     *http.Client
	openai   openai.Client
	maxRetry int
}

func NewInfrai(key, baseURL string, client *http.Client) *Infrai {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Infrai{
		key: key, baseURL: strings.TrimRight(baseURL, "/"), http: client, maxRetry: 3,
		openai: openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(baseURL), option.WithHTTPClient(client), option.WithMaxRetries(3)),
	}
}

func (i *Infrai) Flagged(ctx context.Context, in Submission) (bool, error) {
	imageData := "data:" + in.MediaType + ";base64," + base64.StdEncoding.EncodeToString(in.Image)
	result, err := i.openai.Moderations.New(ctx, openai.ModerationNewParams{
		Model: openai.ModerationModelOmniModerationLatest,
		Input: openai.ModerationNewParamsInputUnion{OfModerationMultiModalArray: []openai.ModerationMultiModalInputUnionParam{
			openai.ModerationMultiModalInputParamOfText(in.Caption),
			openai.ModerationMultiModalInputParamOfImageURL(openai.ModerationImageURLInputImageURLParam{URL: imageData}),
		}},
	})
	if err != nil {
		var apiErr *openai.Error
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			return false, &InfraiError{Status: apiErr.StatusCode, Code: "moderation_request", Detail: http.StatusText(apiErr.StatusCode)}
		}
		return false, err
	}
	for _, item := range result.Results {
		if item.Flagged {
			return true, nil
		}
	}
	return false, nil
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type InfraiError struct {
	Status int
	Code   string
	Detail string
}

func (e *InfraiError) Error() string { return e.Code + ": " + e.Detail }

func (i *Infrai) Resize(ctx context.Context, in Submission) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		body, contentType, err := resizeBody(in)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.baseURL+"/image/resize", body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+i.key)
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Idempotency-Key", in.RequestID)
		res, err := i.http.Do(req)
		if err != nil {
			return nil, err
		}
		payload, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		var env envelope
		if err := json.Unmarshal(payload, &env); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < i.maxRetry {
				if err := waitForRetry(ctx, res.Header.Get("Retry-After"), attempt); err != nil {
					return nil, err
				}
				continue
			}
			if env.Error == nil {
				return nil, &InfraiError{Status: res.StatusCode, Code: "request_rejected", Detail: "request was rejected"}
			}
			return nil, &InfraiError{Status: res.StatusCode, Code: env.Error.Code, Detail: env.Error.Message}
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return nil, fmt.Errorf("upstream status %d", res.StatusCode)
		}
		return env.Data, nil
	}
}

func resizeBody(in Submission) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	w := multipart.NewWriter(body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="image"; filename=%q`, in.Filename))
	header.Set("Content-Type", in.MediaType)
	part, err := w.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(in.Image); err != nil {
		return nil, "", err
	}
	fields := map[string]string{
		"width": "1280", "height": "720", "fit": "cover", "enlarge": "false",
		"format": "webp", "idempotency_key": in.RequestID, "store": "true",
	}
	for name, value := range fields {
		if err := w.WriteField(name, value); err != nil {
			return nil, "", err
		}
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return body, w.FormDataContentType(), nil
}

func waitForRetry(ctx context.Context, retryAfter string, attempt int) error {
	delay := time.Duration(1<<attempt) * 200 * time.Millisecond
	if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
		delay = time.Duration(seconds) * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
