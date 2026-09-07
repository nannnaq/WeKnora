package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestApplyRealtimeContextEnvOverrides(t *testing.T) {
	t.Setenv("REALTIME_CONTEXT_ENABLED", "true")
	t.Setenv("REALTIME_CONTEXT_GRAPH_BASE_URL", "http://graph.internal")
	t.Setenv("REALTIME_CONTEXT_RULE_BASE_URL", "http://rules.internal")
	t.Setenv("REALTIME_CONTEXT_CACHE_TTL", "90s")
	t.Setenv("REALTIME_CONTEXT_REQUEST_TIMEOUT", "7s")
	t.Setenv("REALTIME_CONTEXT_MAX_MATCHED_ENTITIES", "30")
	t.Setenv("REALTIME_CONTEXT_MAX_CONTEXT_CHARS", "65536")

	cfg := &Config{RealtimeContext: &RealtimeContextConfig{Enabled: false}}
	applyRealtimeContextEnvOverrides(cfg)

	require.True(t, cfg.RealtimeContext.Enabled)
	require.Equal(t, "http://graph.internal", cfg.RealtimeContext.GraphBaseURL)
	require.Equal(t, "http://rules.internal", cfg.RealtimeContext.RuleBaseURL)
	require.Equal(t, 90*time.Second, cfg.RealtimeContext.CacheTTL)
	require.Equal(t, 7*time.Second, cfg.RealtimeContext.RequestTimeout)
	require.Equal(t, 30, cfg.RealtimeContext.MaxMatchedEntities)
	require.Equal(t, 65536, cfg.RealtimeContext.MaxContextChars)
}
