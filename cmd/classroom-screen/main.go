package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/example/classroom-upload-screen/internal/screening"
)

type server struct{ workflow *screening.Workflow }

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	client := screening.NewInfrai(key, screening.BaseURL, nil)
	s := &server{workflow: screening.New(client, client, time.Now)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /screen", s.screen)
	log.Printf("classroom screen listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func (s *server) screen(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart request"})
		return
	}
	image, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
		return
	}
	defer image.Close()
	content, err := io.ReadAll(io.LimitReader(image, 10<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot read image"})
		return
	}
	deadline, err := time.Parse(time.RFC3339, r.FormValue("deadline"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "deadline must be RFC3339"})
		return
	}
	decision, err := s.workflow.Screen(r.Context(), screening.Submission{
		CourseID: r.FormValue("course_id"), LearnerID: r.FormValue("learner_id"), Deadline: deadline,
		Caption: r.FormValue("caption"), Filename: header.Filename, MediaType: mediaType(header), Image: content, RequestID: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		var upstream *screening.InfraiError
		if errors.As(err, &upstream) && upstream.Status >= 400 && upstream.Status < 500 {
			writeJSON(w, upstream.Status, map[string]string{"error": upstream.Code, "message": upstream.Detail})
			return
		}
		if strings.Contains(err.Error(), "required") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "screening could not complete"})
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func mediaType(header *multipart.FileHeader) string {
	value := header.Header.Get("Content-Type")
	if value == "" {
		return "application/octet-stream"
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
