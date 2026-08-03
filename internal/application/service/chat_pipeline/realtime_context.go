package chatpipeline

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/realtimecontext"
	"github.com/Tencent/WeKnora/internal/types"
)

// PluginRealtimeContext resolves read-only live evidence for the current turn.
// Resolver failures are represented inside the evidence bundle and never fail
// the overall QA request.
type PluginRealtimeContext struct {
	resolver realtimecontext.Resolver
}

func NewPluginRealtimeContext(eventManager *EventManager, resolver realtimecontext.Resolver) *PluginRealtimeContext {
	plugin := &PluginRealtimeContext{resolver: resolver}
	eventManager.Register(plugin)
	return plugin
}

func (p *PluginRealtimeContext) ActivationEvents() []types.EventType {
	return []types.EventType{types.REALTIME_CONTEXT}
}

func (p *PluginRealtimeContext) OnEvent(
	ctx context.Context,
	_ types.EventType,
	chatManage *types.ChatManage,
	next func() *PluginError,
) *PluginError {
	if p.resolver == nil {
		return next()
	}
	started := time.Now()
	history := make([]realtimecontext.Turn, 0, len(chatManage.History))
	for _, item := range chatManage.History {
		if item == nil {
			continue
		}
		history = append(history, realtimecontext.Turn{User: item.Query, Assistant: item.Answer})
	}
	bundle := p.resolver.Resolve(ctx, realtimecontext.Request{
		Query: chatManage.Query, History: history, ModelID: chatManage.ChatModelID,
	})
	chatManage.RealtimeAttempted = bundle.Attempted
	chatManage.RealtimeUsable = bundle.Usable()
	chatManage.RealtimeContext = bundle.Render()
	pipelineInfo(ctx, "RealtimeContext", "resolved", map[string]interface{}{
		"session_id":     chatManage.SessionID,
		"attempted":      bundle.Attempted,
		"usable":         bundle.Usable(),
		"entity_count":   len(bundle.Entities),
		"rule_set_count": len(bundle.RuleSets),
		"source_count":   len(bundle.Sources),
		"duration_ms":    time.Since(started).Milliseconds(),
	})
	return next()
}
