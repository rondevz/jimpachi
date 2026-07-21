// Package summary adapts a local Ollama instance to Jimpachi summaries.
package summary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrConfiguration identifies invalid or incomplete local Ollama setup.
var ErrConfiguration = errors.New("Ollama configuration")

// Summary is the structured, derived quick view of a Transcription.
type Summary struct {
	Title         string   `json:"title"`
	Overview      string   `json:"overview"`
	Agreements    []string `json:"agreements_decisions"`
	ActionItems   []string `json:"action_items"`
	Deadlines     []string `json:"deadlines"`
	OpenQuestions []string `json:"open_questions"`
}

// Ollama invokes a configured local Ollama generate endpoint.
type Ollama struct {
	Endpoint, Model string
	Client          *http.Client
	RequestTimeout  time.Duration
}

// LoadConfiguredOllama reads optional [ollama] endpoint and model configuration.
func LoadConfiguredOllama() (Ollama, error) {
	home := os.Getenv("XDG_CONFIG_HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return Ollama{}, fmt.Errorf("locate user home for Ollama configuration: %w", err)
		}
		home = filepath.Join(home, ".config")
	}
	b, err := os.ReadFile(filepath.Join(home, "jimpachi", "config.toml"))
	if os.IsNotExist(err) {
		return Ollama{}, nil
	}
	if err != nil {
		return Ollama{}, fmt.Errorf("read Ollama configuration: %w", err)
	}
	var o Ollama
	inside := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "[ollama]" {
			inside = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inside = false
		}
		if !inside || !strings.Contains(line, "=") {
			continue
		}
		p := strings.SplitN(line, "=", 2)
		switch strings.TrimSpace(p[0]) {
		case "endpoint":
			o.Endpoint = strings.Trim(strings.TrimSpace(p[1]), "\"")
		case "model":
			o.Model = strings.Trim(strings.TrimSpace(p[1]), "\"")
		}
	}
	return o, nil
}

// Summarize generates a strictly structured local summary.
func (o Ollama) Summarize(ctx context.Context, transcript string) (Summary, error) {
	if o.Endpoint == "" || o.Model == "" {
		return Summary{}, fmt.Errorf("run Ollama: %w: endpoint and model must be configured", ErrConfiguration)
	}
	endpoint, err := localEndpoint(o.Endpoint)
	if err != nil {
		return Summary{}, fmt.Errorf("run Ollama: %w", err)
	}
	timeout := o.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"model": o.Model, "stream": false, "format": "json", "prompt": "Create JSON only with title, overview, agreements_decisions, action_items, deadlines, open_questions. Omit unavailable content as empty strings or arrays. Transcript:\n" + transcript})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint.String(), "/")+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return Summary{}, fmt.Errorf("create Ollama request: %w", err)
	}
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := clientCopy.Do(req)
	if err != nil {
		return Summary{}, fmt.Errorf("run Ollama: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Summary{}, fmt.Errorf("run Ollama: unexpected response")
	}
	var response struct {
		Response string `json:"response"`
	}
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&response); err != nil {
		return Summary{}, fmt.Errorf("read Ollama response: %w", err)
	}
	if err := requireEnd(decoder); err != nil {
		return Summary{}, fmt.Errorf("read Ollama response: %w", err)
	}
	if response.Response == "" {
		return Summary{}, fmt.Errorf("read Ollama response: summary is required")
	}
	return parseJSON([]byte(response.Response))
}

func parseJSON(b []byte) (Summary, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(b))
	if err := decoder.Decode(&raw); err != nil {
		return Summary{}, err
	}
	if err := requireEnd(decoder); err != nil {
		return Summary{}, err
	}
	allowed := map[string]bool{"title": true, "overview": true, "agreements_decisions": true, "action_items": true, "deadlines": true, "open_questions": true}
	for key := range raw {
		if !allowed[key] {
			return Summary{}, fmt.Errorf("unexpected summary field %q", key)
		}
	}
	decodeString := func(key string, nonEmpty bool) (string, error) {
		value, ok := raw[key]
		if !ok {
			return "", fmt.Errorf("summary %s is required", key)
		}
		var result string
		if err := json.Unmarshal(value, &result); err != nil || (nonEmpty && result == "") {
			return "", fmt.Errorf("summary %s must be a string", key)
		}
		return result, nil
	}
	decodeArray := func(key string) ([]string, error) {
		value, ok := raw[key]
		if !ok {
			return nil, fmt.Errorf("summary %s is required", key)
		}
		var result []string
		if err := json.Unmarshal(value, &result); err != nil || result == nil {
			return nil, fmt.Errorf("summary %s must be an array of strings", key)
		}
		return result, nil
	}
	var s Summary
	var err error
	if s.Title, err = decodeString("title", true); err != nil {
		return Summary{}, err
	}
	if s.Overview, err = decodeString("overview", false); err != nil {
		return Summary{}, err
	}
	if s.Agreements, err = decodeArray("agreements_decisions"); err != nil {
		return Summary{}, err
	}
	if s.ActionItems, err = decodeArray("action_items"); err != nil {
		return Summary{}, err
	}
	if s.Deadlines, err = decodeArray("deadlines"); err != nil {
		return Summary{}, err
	}
	if s.OpenQuestions, err = decodeArray("open_questions"); err != nil {
		return Summary{}, err
	}
	return s, nil
}

func localEndpoint(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("%w: endpoint must be an HTTP loopback URL", ErrConfiguration)
	}
	host := endpoint.Hostname()
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("%w: endpoint must be a loopback URL", ErrConfiguration)
		}
	}
	return endpoint, nil
}

func requireEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
