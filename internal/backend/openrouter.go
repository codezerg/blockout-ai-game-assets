package backend

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

const DefaultBaseURL = "https://openrouter.ai/api/v1"

// OpenRouter calls Chat Completions, the only OpenRouter API that lets text
// and image parts interleave.
type OpenRouter struct {
	Key         string
	BaseURL     string
	HTTP        *http.Client
	MaxAttempts int
	Backoff     []time.Duration // wait before attempt 2, 3, ...
}

func NewOpenRouter(key string) *OpenRouter {
	return &OpenRouter{
		Key:         key,
		BaseURL:     DefaultBaseURL,
		HTTP:        &http.Client{Timeout: 6 * time.Minute},
		MaxAttempts: 8,
		Backoff:     []time.Duration{2 * time.Second, 4 * time.Second, 6 * time.Second},
	}
}

func (c *OpenRouter) Name() string { return "openrouter" }

type chatResponse struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Choices  []struct {
		FinishReason       string `json:"finish_reason"`
		NativeFinishReason string `json:"native_finish_reason"`
		Message            struct {
			Content json.RawMessage `json:"content"`
			Images  []struct {
				ImageURL imageURL `json:"image_url"`
			} `json:"images"`
		} `json:"message"`
		Error *apiError `json:"error"`
	} `json:"choices"`
	Usage *struct {
		Cost *float64 `json:"cost"`
	} `json:"usage"`
	Error *apiError `json:"error"`
}

type apiError struct {
	Code     any             `json:"code"`
	Message  string          `json:"message"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *OpenRouter) Generate(ctx context.Context, req Request) (*Result, error) {
	body, elided, err := buildBody(req)
	if err != nil {
		return nil, err
	}
	var last *Error
	for attempt := 1; attempt <= c.MaxAttempts; attempt++ {
		if attempt > 1 {
			wait := c.Backoff[min(attempt-2, len(c.Backoff)-1)]
			select {
			case <-ctx.Done():
				return nil, &Error{Code: "cancelled", Message: ctx.Err().Error(), Attempts: attempt - 1, RequestJSON: elided}
			case <-time.After(wait):
			}
		}
		res, gerr := c.once(ctx, body)
		if gerr == nil {
			res.Attempts = attempt
			res.RequestJSON = elided
			return res, nil
		}
		gerr.Attempts = attempt
		gerr.RequestJSON = elided
		last = gerr
		if !gerr.Retryable || ctx.Err() != nil {
			break
		}
	}
	if last == nil { // MaxAttempts < 1: returning the nil *Error would read as a non-nil error
		return nil, &Error{Code: "internal", Message: "no attempts made: MaxAttempts is below 1", RequestJSON: elided}
	}
	return nil, last
}

func (c *OpenRouter) once(ctx context.Context, body []byte) (*Result, *Error) {
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Code: "bad_request", Message: err.Error()}
	}
	hreq.Header.Set("Authorization", "Bearer "+c.Key)
	hreq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(hreq)
	if err != nil {
		return nil, &Error{Code: "network", Message: err.Error(), Retryable: ctx.Err() == nil}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &Error{Code: "network", Message: "reading response: " + err.Error(), Retryable: true}
	}
	stored := elideJSON(raw)

	var cr chatResponse
	jerr := json.Unmarshal(raw, &cr)

	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if jerr == nil && cr.Error != nil {
			msg = cr.Error.Message
		}
		return nil, statusError(resp.StatusCode, msg, stored)
	}
	if jerr != nil {
		return nil, &Error{Code: "bad_response", Message: "response is not JSON: " + jerr.Error(), ResponseJSON: stored}
	}
	if cr.Error != nil {
		status := 0
		if f, ok := cr.Error.Code.(float64); ok {
			status = int(f)
		}
		e := statusError(status, cr.Error.Message, stored)
		return nil, e
	}
	if len(cr.Choices) == 0 {
		return nil, &Error{Code: "bad_response", Message: "response has no choices", ResponseJSON: stored}
	}
	ch := cr.Choices[0]
	if ch.Error != nil {
		return nil, &Error{Code: "provider_error", Message: ch.Error.Message, Retryable: true, ResponseJSON: stored}
	}
	text := contentText(ch.Message.Content)
	res := &Result{Text: text, Model: cr.Model, Provider: cr.Provider, ID: cr.ID, ResponseJSON: stored}
	if cr.Usage != nil {
		res.Cost = cr.Usage.Cost
	}
	if len(ch.Message.Images) == 0 {
		msg := fmt.Sprintf("model returned no image (finish_reason %q, native %q)", ch.FinishReason, ch.NativeFinishReason)
		if text != "" {
			msg += "; it said: " + truncate(text, 400)
		}
		return nil, &Error{Code: "no_image", Message: msg, Cost: res.Cost, ResponseJSON: stored}
	}
	img, mime, err := decodeDataURL(ch.Message.Images[0].ImageURL.URL)
	if err != nil {
		return nil, &Error{Code: "bad_response", Message: err.Error(), Cost: res.Cost, ResponseJSON: stored}
	}
	res.Image, res.MIME = img, mime
	return res, nil
}

func statusError(status int, msg string, stored []byte) *Error {
	e := &Error{Code: fmt.Sprintf("http_%d", status), Status: status, Message: truncate(msg, 600), ResponseJSON: stored}
	switch status {
	case 401:
		e.Message = "OpenRouter rejected the key; check OPENROUTER_API_KEY. " + e.Message
	case 402:
		e.Message = "OpenRouter account is out of credits. " + e.Message
	case 403:
		e.Message = "refused: the key's spending limit is reached, or moderation blocked the prompt. " + e.Message
	case 413:
		e.Message = "request too large: use fewer or smaller style refs. " + e.Message
	case 408, 429, 500, 502, 503, 504:
		e.Retryable = true // failed generations are not billed
	case 400:
		// 400 "Request path cannot be relayed" is a DNS issue at the edge,
		// not a fault in the request: it hit 25-75% of requests on
		// 2026-11-25 on every model, provider and body size. The same
		// request then succeeds on retry, and failures are not billed.
		// See EXPERIMENTS.md, E1.
		e.Retryable = strings.Contains(msg, "cannot be relayed")
	}
	return e
}

func contentText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

// elideJSON replaces inline image data in a JSON body with placeholders and
// pretty-prints it. Non-JSON bodies are returned as they are.
func elideJSON(raw []byte) []byte {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for k, x := range t {
				t[k] = walk(x)
			}
		case []any:
			for i, x := range t {
				t[i] = walk(x)
			}
		case string:
			if strings.HasPrefix(t, "data:") && len(t) > 256 {
				if b, _, err := decodeDataURL(t); err == nil {
					return placeholder("image", b)
				}
				return fmt.Sprintf("<elided data URL: %d chars>", len(t))
			}
		}
		return v
	}
	out, err := marshalReadable(walk(v))
	if err != nil {
		return raw
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
