package vlm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteAPIVLMSendsConfiguredThinkingFlag(t *testing.T) {
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"object":"chat.completion",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]
		}`))
	}))
	t.Cleanup(server.Close)
	withVLMSSRFWhitelist(t, "127.0.0.1")

	model, err := NewRemoteAPIVLM(&Config{
		BaseURL:   server.URL + "/v1",
		ModelName: "Qwen3.6-27B-AWQ",
		Extra: map[string]any{
			"enable_thinking": "false",
		},
	})
	if err != nil {
		t.Fatalf("NewRemoteAPIVLM() error = %v", err)
	}

	if _, err := model.Predict(context.Background(), [][]byte{{0xff, 0xd8, 0xff}}, "describe"); err != nil {
		t.Fatalf("Predict() error = %v", err)
	}

	var payload struct {
		ChatTemplateKwargs map[string]any `json:"chat_template_kwargs"`
	}
	if err := json.Unmarshal(<-requestBody, &payload); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	got, ok := payload.ChatTemplateKwargs["enable_thinking"].(bool)
	if !ok || got {
		t.Fatalf("chat_template_kwargs.enable_thinking = %#v; want false", payload.ChatTemplateKwargs["enable_thinking"])
	}
}
