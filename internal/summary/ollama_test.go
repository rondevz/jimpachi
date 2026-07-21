package summary

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaClassifiesInvalidEndpointAsConfigurationError(t *testing.T) {
	for _, endpoint := range []string{"http://example.com", "://bad"} {
		_, err := (Ollama{Endpoint: endpoint, Model: "local"}).Summarize(context.Background(), "private transcript")
		if !errors.Is(err, ErrConfiguration) {
			t.Errorf("Summarize(%q) error = %v, want configuration error", endpoint, err)
		}
	}
}

func TestParseJSONReadsStructuredOllamaSummary(t *testing.T) {
	s, err := parseJSON([]byte(`{"title":"Release plan","overview":"Discussed release.","agreements_decisions":["Ship Friday"],"action_items":["Test"],"deadlines":["Friday"],"open_questions":[]}`))
	if err != nil {
		t.Fatalf("parseJSON() error = %v", err)
	}
	if s.Title != "Release plan" || len(s.ActionItems) != 1 || len(s.OpenQuestions) != 0 {
		t.Errorf("parseJSON() = %#v", s)
	}
}

func TestParseJSONRejectsUnknownAndMissingStructuredFields(t *testing.T) {
	for _, contents := range []string{`{"title":"Plan","overview":"Overview","agreements_decisions":[],"action_items":[],"deadlines":[],"open_questions":[],"extra":true}`, `{"title":"Plan"}`, `{"title":"Plan","overview":"Overview","agreements_decisions":null,"action_items":[],"deadlines":[],"open_questions":[]}`, `{"title":"Plan","overview":"Overview","agreements_decisions":[],"action_items":[],"deadlines":[],"open_questions":[]} trailing`, `{"title":`} {
		if _, err := parseJSON([]byte(contents)); err == nil {
			t.Errorf("parseJSON(%s) error = nil", contents)
		}
	}
}

func TestOllamaRejectsRemoteEndpoint(t *testing.T) {
	if _, err := (Ollama{Endpoint: "http://example.com", Model: "local"}).Summarize(context.Background(), "private transcript"); err == nil {
		t.Fatal("Summarize() error = nil")
	}
}

func TestOllamaRejectsRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com", http.StatusFound)
	}))
	defer server.Close()
	if _, err := (Ollama{Endpoint: server.URL, Model: "local"}).Summarize(context.Background(), "private transcript"); err == nil {
		t.Fatal("Summarize() error = nil")
	}
}

func TestOllamaTimesOutHungRequest(t *testing.T) {
	cleanup := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-cleanup:
		}
	}))
	defer server.Close()
	defer close(cleanup)
	started := time.Now()
	_, err := (Ollama{Endpoint: server.URL, Model: "local", RequestTimeout: 10 * time.Millisecond}).Summarize(context.Background(), "private transcript")
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("Summarize() = %v after %s", err, time.Since(started))
	}
}
