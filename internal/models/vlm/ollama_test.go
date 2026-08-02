package vlm

import "testing"

func TestBuildChatRequestDisablesThinking(t *testing.T) {
	vlm := &OllamaVLM{modelName: "qwen3.6:27b"}

	req := vlm.buildChatRequest([][]byte{{1, 2, 3}}, "describe this image")

	if req.Think == nil {
		t.Fatal("buildChatRequest omitted think; want explicit false")
	}
	if enabled, ok := req.Think.Value.(bool); !ok || enabled {
		t.Fatalf("buildChatRequest think = %#v; want false", req.Think.Value)
	}
}

func TestBuildChatRequestPreservesModelPromptAndImages(t *testing.T) {
	vlm := &OllamaVLM{modelName: "qwen3.6:27b"}
	firstImage := []byte{1, 2, 3}
	secondImage := []byte{4, 5, 6}

	req := vlm.buildChatRequest(
		[][]byte{firstImage, nil, secondImage},
		"describe these images",
	)

	if req.Model != "qwen3.6:27b" {
		t.Fatalf("buildChatRequest model = %q; want qwen3.6:27b", req.Model)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content != "describe these images" {
		t.Fatalf("buildChatRequest messages = %#v; want one user prompt", req.Messages)
	}
	if len(req.Messages[0].Images) != 2 {
		t.Fatalf("buildChatRequest image count = %d; want 2", len(req.Messages[0].Images))
	}
	if string(req.Messages[0].Images[0]) != string(firstImage) || string(req.Messages[0].Images[1]) != string(secondImage) {
		t.Fatalf("buildChatRequest images = %#v; want non-empty images in order", req.Messages[0].Images)
	}
}
