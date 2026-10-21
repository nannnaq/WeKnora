package chat

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
	"strings"
)

// NewRemoteChat builds the protocol client and applies explicit model policy to every call.
func NewRemoteChat(config *ChatConfig) (Chat, error) {
	client, err := newRemoteChat(config)
	if err == nil && strings.EqualFold(strings.TrimSpace(config.ExtraConfig["enable_thinking"]), "false") {
		client = &thinkingDisabledChat{inner: client}
	}
	return client, err
}

type thinkingDisabledChat struct{ inner Chat }

func withoutThinking(opts *ChatOptions) *ChatOptions {
	local := ChatOptions{}
	if opts != nil {
		local = *opts
	}
	disabled := false
	local.Thinking = &disabled
	local.ReasoningEffort = "none"
	return &local
}

func (c *thinkingDisabledChat) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*types.ChatResponse, error) {
	return c.inner.Chat(ctx, messages, withoutThinking(opts))
}

func (c *thinkingDisabledChat) ChatStream(ctx context.Context, messages []Message, opts *ChatOptions) (<-chan types.StreamResponse, error) {
	return c.inner.ChatStream(ctx, messages, withoutThinking(opts))
}

func (c *thinkingDisabledChat) GetModelName() string { return c.inner.GetModelName() }
func (c *thinkingDisabledChat) GetModelID() string   { return c.inner.GetModelID() }
