package config_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestDefaultKnowledgeAnswerPromptsUseAdaptiveBrevity(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	t.Chdir(filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..")))

	viper.Reset()
	t.Cleanup(viper.Reset)

	cfg, err := config.LoadConfig()
	require.NoError(t, err)
	reasoningAgent := config.FindTemplateByID(cfg.PromptTemplates, "progressive_rag_agent")
	require.NotNil(t, reasoningAgent)

	prompts := map[string]string{
		"quick answer":    cfg.Conversation.Summary.Prompt,
		"reasoning agent": reasoningAgent.Content,
	}
	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, prompt, "Answer the user's question directly before adding supporting detail.")
			require.Contains(t, prompt, "For simple questions, prefer 1-2 short paragraphs or 3-5 bullets.")
			require.Contains(t, prompt, "For complex questions, preserve the essential reasoning, conditions, steps, and risks")
			require.Contains(t, prompt, "Do not restate the question, repeat the conclusion, or add tangential background.")
			require.Contains(t, prompt, "If the user explicitly asks for a detailed answer, provide the requested detail.")
		})
	}
}
