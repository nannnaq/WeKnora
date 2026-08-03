package service

import (
	"context"
	"testing"

	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type testPipelinePlugin struct {
	events []types.EventType
	handle func(*types.ChatManage) *chatpipeline.PluginError
}

func (p *testPipelinePlugin) ActivationEvents() []types.EventType { return p.events }
func (p *testPipelinePlugin) OnEvent(
	_ context.Context,
	_ types.EventType,
	chatManage *types.ChatManage,
	next func() *chatpipeline.PluginError,
) *chatpipeline.PluginError {
	if p.handle != nil {
		if err := p.handle(chatManage); err != nil {
			return err
		}
	}
	return next()
}

func TestKnowledgeQAContinuesToAnswerWhenKBIsEmptyButRealtimeWasAttempted(t *testing.T) {
	manager := chatpipeline.NewEventManager()
	manager.Register(&testPipelinePlugin{
		events: []types.EventType{types.CHUNK_SEARCH_PARALLEL},
		handle: func(*types.ChatManage) *chatpipeline.PluginError { return chatpipeline.ErrSearchNothing },
	})
	composed := false
	answered := false
	manager.Register(&testPipelinePlugin{
		events: []types.EventType{types.INTO_CHAT_MESSAGE},
		handle: func(*types.ChatManage) *chatpipeline.PluginError { composed = true; return nil },
	})
	manager.Register(&testPipelinePlugin{
		events: []types.EventType{types.CHAT_COMPLETION},
		handle: func(*types.ChatManage) *chatpipeline.PluginError { answered = true; return nil },
	})
	service := &sessionService{eventManager: manager}
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{SessionID: "session-1", Query: "现在的交战规则是什么？"},
		PipelineState:   types.PipelineState{RealtimeAttempted: true},
	}

	err := service.KnowledgeQAByEvent(context.Background(), chatManage, []types.EventType{
		types.CHUNK_SEARCH_PARALLEL,
		types.CHUNK_RERANK,
		types.CHUNK_MERGE,
		types.FILTER_TOP_K,
		types.INTO_CHAT_MESSAGE,
		types.CHAT_COMPLETION,
	})

	require.NoError(t, err)
	require.True(t, composed)
	require.True(t, answered)
}
