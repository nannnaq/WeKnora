package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/rerank"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type fixedScoreReranker struct {
	scores []float64
}

func (r fixedScoreReranker) Rerank(
	_ context.Context, _ string, documents []string,
) ([]rerank.RankResult, error) {
	results := make([]rerank.RankResult, 0, len(r.scores))
	for i, score := range r.scores {
		if i >= len(documents) {
			break
		}
		results = append(results, rerank.RankResult{Index: i, RelevanceScore: score})
	}
	return results, nil
}

func (fixedScoreReranker) GetModelName() string { return "fixed-score" }
func (fixedScoreReranker) GetModelID() string   { return "fixed-score" }

func TestRerankFallbackMinScoreForExplicitScope(t *testing.T) {
	if got := rerankFallbackMinScore(nil); got != 0.15 {
		t.Fatalf("default fallback minimum = %v, want 0.15", got)
	}

	targets := types.SearchTargets{{DisableRecallThresholds: true}}
	if got := rerankFallbackMinScore(targets); got != 0 {
		t.Fatalf("explicit-scope fallback minimum = %v, want 0", got)
	}
}

func TestRerankDoesNotForceLowRelevanceKnowledgeIntoUsableRealtimeAnswer(t *testing.T) {
	candidate := &types.SearchResult{
		ID:             "jma-manual",
		KnowledgeTitle: "JMA5312-6-Users-Manual-1-991971.pdf",
		Content:        "Radar user map and navigation functions",
		Score:          1,
	}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankThreshold: 0.3},
		PipelineState: types.PipelineState{
			RealtimeAttempted: true,
			RealtimeUsable:    true,
			SearchResult:      []*types.SearchResult{candidate},
		},
	}

	got, err := (&PluginRerank{}).rerank(
		context.Background(),
		manage,
		fixedScoreReranker{scores: []float64{0.2266}},
		"现在的交战规则是什么",
		[]string{candidate.Content},
		[]*types.SearchResult{candidate},
	)

	require.NoError(t, err)
	require.Empty(t, got, "low-relevance KB material must not be forced into a usable realtime answer")
}

func TestRerankKeepsRelevantKnowledgeAlongsideUsableRealtimeAnswer(t *testing.T) {
	candidate := &types.SearchResult{
		ID:             "engagement-rules",
		KnowledgeTitle: "交战规则说明.pdf",
		Content:        "当前生效的交战规则包括 HR_001 和 SR_001",
		Score:          1,
	}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankThreshold: 0.3},
		PipelineState: types.PipelineState{
			RealtimeAttempted: true,
			RealtimeUsable:    true,
			SearchResult:      []*types.SearchResult{candidate},
		},
	}

	got, err := (&PluginRerank{}).rerank(
		context.Background(),
		manage,
		fixedScoreReranker{scores: []float64{0.926}},
		"现在的交战规则是什么",
		[]string{candidate.Content},
		[]*types.SearchResult{candidate},
	)

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 0, got[0].Index)
}

func TestRerankPreservesTopOneFallbackForKnowledgeOnlyAnswer(t *testing.T) {
	candidate := &types.SearchResult{
		ID:             "weak-kb-hit",
		KnowledgeTitle: "manual.pdf",
		Content:        "weakly related material",
		Score:          1,
	}
	manage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{RerankThreshold: 0.3},
		PipelineState: types.PipelineState{
			SearchResult: []*types.SearchResult{candidate},
		},
	}

	got, err := (&PluginRerank{}).rerank(
		context.Background(),
		manage,
		fixedScoreReranker{scores: []float64{0.2266}},
		"manual question",
		[]string{candidate.Content},
		[]*types.SearchResult{candidate},
	)

	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestRerankWithoutModelDropsKnowledgeWhenRealtimeIsUsable(t *testing.T) {
	manage := &types.ChatManage{
		PipelineState: types.PipelineState{
			RealtimeAttempted: true,
			RealtimeUsable:    true,
			SearchResult: []*types.SearchResult{{
				ID: "irrelevant-manual",
			}},
		},
	}

	err := (&PluginRerank{}).OnEvent(
		context.Background(),
		types.CHUNK_RERANK,
		manage,
		func() *PluginError { return nil },
	)

	require.Equal(t, ErrSearchNothing, err)
	require.Empty(t, manage.SearchResult)
}
