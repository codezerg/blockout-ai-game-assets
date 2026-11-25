// Package backend sends one generation request to an image model. The model
// sits behind the Backend interface so that everything except generation can
// run, and be tested, for free against Fake.
package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Part is one piece of message content: text, or image bytes.
type Part struct {
	Text  string
	Image []byte
	MIME  string // of Image
	Label string // names the image in logs: "sketch", "style_ref_1"
}

type Request struct {
	Model  string
	Aspect string // "1:1", "16:9", ...
	Size   string // "1K", "2K", "4K"; "0.5K" on some models
	Parts  []Part
}

type Result struct {
	Image    []byte
	MIME     string
	Text     string   // any text the model returned alongside the image
	Model    string   // model that actually served the request
	Provider string   // provider that served it, e.g. "Google AI Studio"
	ID       string   // provider generation id
	Cost     *float64 // USD, when the provider reports it
	Attempts int
	// Exact request and response bodies with image data replaced by a
	// placeholder naming the stored file and its hash.
	RequestJSON  []byte
	ResponseJSON []byte
}

type Backend interface {
	Name() string
	Generate(ctx context.Context, req Request) (*Result, error)
}

// Error is a failed generation, with enough detail to decide what to change.
type Error struct {
	Code      string `json:"code"` // http_502, no_image, bad_response, network, ...
	Status    int    `json:"status,omitempty"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Attempts  int    `json:"attempts"`
	// Cost is what the provider billed for a call that answered without a
	// usable image (USD, when reported). Most failures are not billed.
	Cost *float64 `json:"cost_usd,omitempty"`
	// Response body (image data elided), for the run directory.
	ResponseJSON []byte `json:"-"`
	RequestJSON  []byte `json:"-"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// --- OpenRouter Chat Completions wire format ---

type chatRequest struct {
	Model       string       `json:"model"`
	Modalities  []string     `json:"modalities"`
	ImageConfig *imageConfig `json:"image_config,omitempty"`
	Messages    []message    `json:"messages"`
	Usage       *usageOpt    `json:"usage,omitempty"`
}

type imageConfig struct {
	AspectRatio string `json:"aspect_ratio,omitempty"`
	ImageSize   string `json:"image_size,omitempty"`
}

type usageOpt struct {
	Include bool `json:"include"`
}

type message struct {
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

// buildBody returns the request body, and the same body with image data
// replaced by placeholders for storage.
func buildBody(req Request) (body, elided []byte, err error) {
	mk := func(elide bool) chatRequest {
		cr := chatRequest{
			Model:       req.Model,
			Modalities:  []string{"image", "text"},
			ImageConfig: &imageConfig{AspectRatio: req.Aspect, ImageSize: req.Size},
			Usage:       &usageOpt{Include: true},
		}
		var content []contentPart
		for _, p := range req.Parts {
			if p.Image == nil {
				content = append(content, contentPart{Type: "text", Text: p.Text})
				continue
			}
			url := "data:" + p.MIME + ";base64," + base64.StdEncoding.EncodeToString(p.Image)
			if elide {
				url = placeholder(p.Label, p.Image)
			}
			content = append(content, contentPart{Type: "image_url", ImageURL: &imageURL{URL: url}})
		}
		cr.Messages = []message{{Role: "user", Content: content}}
		return cr
	}
	if body, err = json.Marshal(mk(false)); err != nil {
		return nil, nil, err
	}
	elided, err = marshalReadable(mk(true))
	return body, elided, err
}

// marshalReadable pretty-prints JSON without escaping <, > and &.
func marshalReadable(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func placeholder(label string, data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("<elided %s: %d bytes, sha256 %s>", label, len(data), hex.EncodeToString(sum[:8]))
}

// BuildRequestJSON is the stored (elided) request body, for dry runs.
func BuildRequestJSON(req Request) ([]byte, error) {
	_, elided, err := buildBody(req)
	return elided, err
}

// SniffMIME returns the image MIME type for file bytes.
func SniffMIME(data []byte) string {
	m := http.DetectContentType(data)
	if strings.HasPrefix(m, "image/") {
		return m
	}
	return "image/png"
}

// decodeDataURL splits "data:image/png;base64,...." into bytes and MIME.
func decodeDataURL(u string) ([]byte, string, error) {
	rest, ok := strings.CutPrefix(u, "data:")
	if !ok {
		return nil, "", fmt.Errorf("image is not a data URL (starts %.40q)", u)
	}
	meta, data, ok := strings.Cut(rest, ",")
	if !ok || !strings.HasSuffix(meta, ";base64") {
		return nil, "", fmt.Errorf("image data URL is not base64")
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, "", fmt.Errorf("image base64: %w", err)
	}
	return b, strings.TrimSuffix(meta, ";base64"), nil
}
