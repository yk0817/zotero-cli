// Package embed turns text into vectors via an OpenAI-compatible
// POST {BaseURL}/embeddings endpoint. The same client talks to a local server
// (Ollama / llama.cpp / LM Studio) or a remote API (OpenAI / Gemini-compatible);
// only BaseURL and APIKey differ. There is deliberately no SSRF dial guard here
// (unlike oapdf): the base URL comes from the user's own config and the default
// target is localhost, which such a guard would block.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const requestTimeout = 60 * time.Second

// Embedder turns a batch of texts into vectors. One small method, injectable so
// callers depend on the interface and tests use a deterministic fake.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// HTTPEmbedder is an Embedder backed by an OpenAI-compatible HTTP endpoint.
type HTTPEmbedder struct {
	BaseURL    string
	Model      string
	APIKey     string // optional; sent as Authorization: Bearer when non-empty
	HTTPClient *http.Client
}

// New returns an HTTPEmbedder with a bounded-timeout client.
func New(baseURL, model, apiKey string) *HTTPEmbedder {
	return &HTTPEmbedder{
		BaseURL:    baseURL,
		Model:      model,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: requestTimeout},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed sends inputs in a single request and returns one vector per input, in
// order. An empty input slice returns nil without a request.
func (e *HTTPEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Model: e.Model, Input: inputs})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(e.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	client := e.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: request to %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embed: reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embed: HTTP %d from %s", resp.StatusCode, endpoint)
	}

	var parsed embedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("embed: invalid response from %s: %w", endpoint, err)
	}
	if len(parsed.Data) != len(inputs) {
		return nil, fmt.Errorf("embed: got %d vectors for %d inputs", len(parsed.Data), len(inputs))
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
