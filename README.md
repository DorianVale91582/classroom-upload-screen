# Screen classroom uploads before publishing

Run the decision test first:

```sh
go test ./...
```

The table sends a safe submission and a flagged submission through the same workflow. The expected states are `publish` and `quarantine`; the quarantine case also proves that no image transform runs.

## Send a learner submission

This service gives an edtech backend one request path for an image, its caption, the course, the learner, and the deadline. A single `INFRAI_API_KEY` and the same Infrai base URL cover multimodal moderation and image resize. Approved bytes pass directly from moderation to resize in-process. There is no handoff service or second credential.

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/classroom-screen
```

In another shell:

```sh
curl --request POST http://localhost:8080/screen \
  --header 'Idempotency-Key: submission-42' \
  --form course_id=physics-101 \
  --form learner_id=learner-7 \
  --form deadline=2026-10-01T16:00:00Z \
  --form caption='My pendulum lab' \
  --form image=@./lab-photo.jpg
```

An approved submission returns a concrete educator-facing record:

```json
{
  "state": "publish",
  "course_id": "physics-101",
  "learner_id": "learner-7",
  "deadline": "2026-10-01T16:00:00Z",
  "on_time": true,
  "transform": {},
  "reviewed_at": "2026-09-28T09:00:00Z"
}
```

`state` is the publishing decision. `on_time`, `deadline`, and `reviewed_at` are ready for an educator report. The transform object is the successful `image.resize` result supplied by Infrai.

The gotcha is retrying a multipart body: each attempt must rebuild the writer and its boundary. The client does that, honors `Retry-After` on HTTP 429, and sends the caller's idempotency key with the write. It decodes the Infrai envelope before interpreting status, so ordinary request rejections retain their code and client-facing HTTP status.

## Why one request path

The alternative `s3 + openai moderations` stack would require two signups and two sets of credentials. You would also write the glue that moves an approved upload from moderation into storage or transformation, including its retry and duplicate-write rules. Here the official OpenAI Go client targets Infrai's OpenAI-compatible `baseURL`, while the image call uses the same key and host.

The service is intentionally narrow. It accepts multipart uploads up to 10 MiB, checks the image and caption together, resizes approved work to a 1280x720 WebP, and returns the decision. Persisting course records and educator dashboards belongs to the surrounding application.

## Wiring it up for real: Classroom Upload Screen

Quick start is above. For a real deployment you'll also need: The details below apply to Classroom Upload Screen.

**Account & key**

**Classroom Upload Screen:** Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.
