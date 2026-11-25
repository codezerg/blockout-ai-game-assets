package backend

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func server(t *testing.T, replies ...func(w http.ResponseWriter)) (*OpenRouter, *[]map[string]any) {
	t.Helper()
	var got []map[string]any
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("bad request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		got = append(got, m)
		replies[min(n, len(replies)-1)](w)
		n++
	}))
	t.Cleanup(srv.Close)
	c := NewOpenRouter("k")
	c.BaseURL = srv.URL
	c.Backoff = []time.Duration{time.Millisecond}
	return c, &got
}

var pngBytes = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 400))

func okReply(w http.ResponseWriter) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)
	fmt.Fprintf(w, `{"id":"gen-1","model":"google/m","choices":[{"finish_reason":"stop","message":{"content":"here","images":[{"type":"image_url","image_url":{"url":%q}}]}}],"usage":{"cost":0.039}}`, url)
}

func req() Request {
	return Request{Model: "google/m", Aspect: "1:1", Size: "1K", Parts: []Part{
		{Text: "hello"}, {Image: pngBytes, MIME: "image/png", Label: "sketch"},
	}}
}

func TestRetryOn502ThenSuccess(t *testing.T) {
	c, got := server(t,
		func(w http.ResponseWriter) {
			w.WriteHeader(502)
			fmt.Fprint(w, `{"error":{"code":502,"message":"upstream"}}`)
		},
		okReply)
	res, err := c.Generate(context.Background(), req())
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 2 || res.Cost == nil || *res.Cost != 0.039 || res.Text != "here" || string(res.Image) != string(pngBytes) {
		t.Errorf("result %+v", res)
	}
	first := (*got)[0]
	if first["modalities"].([]any)[0] != "image" || first["image_config"].(map[string]any)["image_size"] != "1K" {
		t.Errorf("body %v", first)
	}
	if strings.Contains(string(res.ResponseJSON), "iVBOR") || !strings.Contains(string(res.ResponseJSON), "<elided image") {
		t.Errorf("response not elided: %s", res.ResponseJSON)
	}
	if !strings.Contains(string(res.RequestJSON), "<elided sketch") {
		t.Errorf("request not elided: %s", res.RequestJSON)
	}
}

func TestNoImageIsNotRetried(t *testing.T) {
	c, got := server(t, func(w http.ResponseWriter) {
		fmt.Fprint(w, `{"id":"g","choices":[{"finish_reason":"stop","message":{"content":"I cannot draw that"}}]}`)
	})
	_, err := c.Generate(context.Background(), req())
	e, ok := err.(*Error)
	if !ok || e.Code != "no_image" || e.Retryable || !strings.Contains(e.Message, "cannot draw") || len(*got) != 1 {
		t.Errorf("err %v, calls %d", err, len(*got))
	}
}

func TestAuthErrorNotRetried(t *testing.T) {
	c, got := server(t, func(w http.ResponseWriter) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"code":401,"message":"no auth"}}`)
	})
	_, err := c.Generate(context.Background(), req())
	e, ok := err.(*Error)
	if !ok || e.Code != "http_401" || len(*got) != 1 {
		t.Errorf("err %v calls %d", err, len(*got))
	}
}

func TestRelayErrorIsRetried(t *testing.T) {
	relay := func(w http.ResponseWriter) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"code":400,"message":"Request path cannot be relayed"}}`)
	}
	c, got := server(t, relay, relay, okReply)
	res, err := c.Generate(context.Background(), req())
	if err != nil || res.Attempts != 3 || len(*got) != 3 {
		t.Fatalf("err %v calls %d", err, len(*got))
	}
	c, got = server(t, func(w http.ResponseWriter) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"code":400,"message":"invalid aspect_ratio"}}`)
	})
	if _, err := c.Generate(context.Background(), req()); err == nil || len(*got) != 1 {
		t.Errorf("plain 400 should not be retried: err %v calls %d", err, len(*got))
	}
}
