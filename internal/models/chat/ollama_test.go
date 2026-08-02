package chat

import (
	"net/http"
	"net/http/httptest"
	"testing"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestBuildChatRequestDisablesThinkingByDefault(t *testing.T) {
	chat := &OllamaChat{modelName: "qwen3.6:27b"}

	req := chat.buildChatRequest([]Message{{Role: "user", Content: "hello"}}, nil, false)

	if req.Think == nil {
		t.Fatal("buildChatRequest omitted think; want explicit false")
	}
	if enabled, ok := req.Think.Value.(bool); !ok || enabled {
		t.Fatalf("buildChatRequest think = %#v; want false", req.Think.Value)
	}
}

func TestBuildChatRequestHonorsExplicitThinking(t *testing.T) {
	chat := &OllamaChat{modelName: "qwen3.6:27b"}
	thinking := true

	req := chat.buildChatRequest(
		[]Message{{Role: "user", Content: "hello"}},
		&ChatOptions{Thinking: &thinking},
		false,
	)

	if req.Think == nil {
		t.Fatal("buildChatRequest omitted explicit think value")
	}
	if enabled, ok := req.Think.Value.(bool); !ok || !enabled {
		t.Fatalf("buildChatRequest think = %#v; want true", req.Think.Value)
	}
}

func TestResolveImageForOllamaRejectsInternalURL(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	if data := resolveImageForOllama("http://169.254.169.254/latest/meta-data/"); data != nil {
		t.Fatalf("resolveImageForOllama returned data for blocked internal URL")
	}
}

func TestResolveImageForOllamaBlocksRedirectToInternalURL(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer server.Close()

	if data := resolveImageForOllama(server.URL); data != nil {
		t.Fatalf("resolveImageForOllama returned data after redirect to blocked internal URL")
	}
}
