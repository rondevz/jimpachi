package summary

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaRequestsStringArraysWithJSONSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Format struct {
				Type       string   `json:"type"`
				Required   []string `json:"required"`
				Additional *bool    `json:"additionalProperties"`
				Properties map[string]struct {
					Type      string `json:"type"`
					MinLength int    `json:"minLength"`
					Items     struct {
						Type string `json:"type"`
					} `json:"items"`
				} `json:"properties"`
			} `json:"format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.Format.Type != "object" || len(request.Format.Properties) != 6 {
			t.Errorf("format root = %#v, want object with six properties", request.Format)
		}
		if request.Format.Additional == nil || *request.Format.Additional {
			t.Errorf("additionalProperties = %v, want false", request.Format.Additional)
		}
		required := make(map[string]bool, len(request.Format.Required))
		for _, field := range request.Format.Required {
			required[field] = true
		}
		for _, field := range []string{"title", "overview", "agreements_decisions", "action_items", "deadlines", "open_questions"} {
			if !required[field] {
				t.Errorf("required = %#v, missing %q", request.Format.Required, field)
			}
		}
		if title, overview := request.Format.Properties["title"], request.Format.Properties["overview"]; title.Type != "string" || title.MinLength != 1 || overview.Type != "string" {
			t.Errorf("title/overview schema = %#v, %#v", title, overview)
		}
		for _, field := range []string{"agreements_decisions", "action_items", "deadlines", "open_questions"} {
			property := request.Format.Properties[field]
			if property.Type != "array" || property.Items.Type != "string" {
				t.Errorf("format property %q = %#v, want array of strings", field, property)
			}
		}
		_, _ = w.Write([]byte(`{"response":"{\"title\":\"Plan\",\"overview\":\"\",\"agreements_decisions\":[],\"action_items\":[],\"deadlines\":[],\"open_questions\":[]}"}`))
	}))
	defer server.Close()
	if _, err := (Ollama{Endpoint: server.URL, Model: "local"}).Summarize(context.Background(), "private transcript"); err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
}

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
