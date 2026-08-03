package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/realtimecontext"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type stubRealtimeResolver struct {
	request realtimecontext.Request
	bundle  realtimecontext.Bundle
}

func (s *stubRealtimeResolver) Resolve(_ context.Context, request realtimecontext.Request) realtimecontext.Bundle {
	s.request = request
	return s.bundle
}

func TestPluginRealtimeContextResolvesCurrentTurnAndStoresSanitizedEvidence(t *testing.T) {
	resolver := &stubRealtimeResolver{bundle: realtimecontext.Bundle{
		Attempted: true,
		Entities: []realtimecontext.EntityEvidence{{
			ID: "camera_004", Name: "远遥码头-可见光", Type: "CAMERA",
		}},
	}}
	manager := NewEventManager()
	NewPluginRealtimeContext(manager, resolver)
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{Query: "它现在在线吗？", ChatModelID: "model-1"},
		PipelineState:   types.PipelineState{History: []*types.History{{Query: "远遥相机参数", Answer: "..."}}},
	}

	err := manager.Trigger(context.Background(), types.REALTIME_CONTEXT, chatManage)

	require.Nil(t, err)
	require.True(t, chatManage.RealtimeAttempted)
	require.True(t, chatManage.RealtimeUsable)
	require.Contains(t, chatManage.RealtimeContext, "远遥码头-可见光")
	require.Equal(t, "model-1", resolver.request.ModelID)
	require.Len(t, resolver.request.History, 1)
}

func TestIntoChatMessageIncludesRealtimeEvidenceWithoutKnowledgeRetrieval(t *testing.T) {
	plugin := &PluginIntoChatMessage{}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:                  "现在的交战规则是什么？",
			SkipKnowledgeRetrieval: true,
			SummaryConfig: types.SummaryConfig{
				ContextTemplate: "问题：{{query}}\n证据：{{contexts}}",
			},
		},
		PipelineState: types.PipelineState{
			RealtimeContext:   "<realtime_evidence>active rules</realtime_evidence>",
			RealtimeAttempted: true,
			RealtimeUsable:    true,
		},
	}

	err := plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, chatManage, func() *PluginError { return nil })

	require.Nil(t, err)
	require.Equal(t, chatManage.RealtimeContext, chatManage.RenderedContexts)
	require.Contains(t, chatManage.UserContent, "active rules")
	require.Contains(t, chatManage.UserContent, "现在的交战规则是什么")
}

func TestIntoChatMessagePreservesPureChatWhenRealtimeIsIrrelevant(t *testing.T) {
	plugin := &PluginIntoChatMessage{}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:                  "帮我写一句欢迎语",
			SkipKnowledgeRetrieval: true,
			SummaryConfig: types.SummaryConfig{
				ContextTemplate: "问题：{{query}}\n证据：{{contexts}}",
			},
		},
	}

	err := plugin.OnEvent(context.Background(), types.INTO_CHAT_MESSAGE, chatManage, func() *PluginError { return nil })

	require.Nil(t, err)
	require.Equal(t, "帮我写一句欢迎语", chatManage.UserContent)
	require.Empty(t, chatManage.RenderedContexts)
}
