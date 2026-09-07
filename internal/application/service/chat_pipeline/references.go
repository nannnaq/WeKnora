package chatpipeline

import (
	"context"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
)

const apiHybridContextRuneBudget = 112_000

// prepareMessagesWithModelContext replaces positional retrieval IDs with
// request-local model handles. Persisted rendered_content remains unchanged;
// public citations are expanded only when the request setting enables them.
func prepareMessagesWithModelContext(
	ctx context.Context,
	chatManage *types.ChatManage,
) ([]chat.Message, *modelcontext.Registry) {
	citationsEnabled := chatManage == nil || chatManage.CitationsEnabled()
	registry := modelcontext.NewRegistry(citationsEnabled)
	if chatManage == nil {
		return nil, registry
	}
	messages := prepareMessagesWithHistory(chatManage)
	if len(messages) > 0 && !chatManage.APIHybridPureAnswer {
		messages[0].Content = strings.TrimRight(messages[0].Content, " \t\r\n") + registry.ProtocolPrompt()
	}
	if len(messages) == 0 {
		return messages, registry
	}

	ordered := orderedPipelineReferences(chatManage)
	if chatManage.APIHybridPureAnswer {
		ordered = trimAPIHybridKnowledgeContext(ctx, chatManage, ordered)
	}
	knowledgeResults := make([]*types.SearchResult, 0, len(ordered))
	knowledgeRows := make([]map[string]interface{}, 0, len(ordered))
	webRows := make([]map[string]interface{}, 0)
	for _, result := range ordered {
		if result == nil {
			continue
		}
		if isPipelineWebReference(result) {
			if chatManage.APIHybridPureAnswer {
				webRows = append(webRows, map[string]interface{}{
					"snippet": stripAPIHybridSourceArtifacts(result.Content),
				})
				continue
			}
			webRows = append(webRows, map[string]interface{}{
				"url":          result.ID,
				"title":        firstPipelineTitle(result),
				"snippet":      result.Content,
				"published_at": result.Metadata["published_at"],
			})
			continue
		}
		knowledgeResults = append(knowledgeResults, result)
		content := getEnrichedPassageForChat(ctx, result)
		if chatManage.APIHybridPureAnswer {
			content = stripAPIHybridSourceArtifacts(content)
			knowledgeRows = append(knowledgeRows, map[string]interface{}{"content": content})
			continue
		}
		knowledgeRows = append(knowledgeRows, map[string]interface{}{
			"chunk_id":          result.ID,
			"knowledge_id":      result.KnowledgeID,
			"knowledge_base_id": result.KnowledgeBaseID,
			"knowledge_title":   firstPipelineTitle(result),
			"chunk_index":       result.ChunkIndex,
			"chunk_type":        result.ChunkType,
			"content":           content,
		})
	}
	modelContexts := ""
	if chatManage.APIHybridPureAnswer {
		modelContexts = renderAPIHybridModelContext(knowledgeRows, webRows)
	} else {
		registry.RegisterSearchResults(knowledgeResults)
		var contextParts []string
		if len(knowledgeRows) > 0 {
			contextParts = append(contextParts, registry.ModelToolResult(&types.ToolResult{
				Success: true,
				Data: map[string]interface{}{
					"display_type": "search_results",
					"results":      knowledgeRows,
				},
			}))
		}
		if len(webRows) > 0 {
			contextParts = append(contextParts, registry.ModelToolResult(&types.ToolResult{
				Success: true,
				Data: map[string]interface{}{
					"display_type": "web_search_results",
					"results":      webRows,
				},
			}))
		}
		modelContexts = strings.Join(contextParts, "\n")
	}
	realtimeContext := strings.TrimSpace(chatManage.RealtimeContext)
	combinedParts := make([]string, 0, 2)
	if strings.TrimSpace(modelContexts) != "" {
		combinedParts = append(combinedParts, modelContexts)
	}
	if realtimeContext != "" {
		combinedParts = append(combinedParts, realtimeContext)
	}
	combinedContexts := strings.Join(combinedParts, "\n\n")
	if combinedContexts == "" {
		return messages, registry
	}

	last := len(messages) - 1
	replaced := false
	realtimeInjected := false
	injectionTarget := ""
	for _, target := range []struct {
		index int
		name  string
	}{{index: 0, name: "system"}, {index: last, name: "user"}} {
		index := target.index
		if chatManage.RenderedContexts != "" && strings.Contains(messages[index].Content, chatManage.RenderedContexts) {
			if realtimeContext != "" && !realtimeInjected {
				before, after, _ := strings.Cut(messages[index].Content, chatManage.RenderedContexts)
				messages[index].Content = before + combinedContexts +
					strings.ReplaceAll(after, chatManage.RenderedContexts, modelContexts)
				realtimeInjected = true
				injectionTarget = target.name
			} else {
				messages[index].Content = strings.ReplaceAll(messages[index].Content, chatManage.RenderedContexts, modelContexts)
			}
			replaced = true
		}
	}
	if !replaced {
		messages[last].Content = combinedContexts + "\n\n" + messages[last].Content
		if realtimeContext != "" {
			realtimeInjected = true
			injectionTarget = "user_fallback"
		}
	}
	pipelineInfo(ctx, "ModelContext", "prepared", map[string]interface{}{
		"injection_target":     injectionTarget,
		"knowledge_result_cnt": len(knowledgeRows) + len(webRows),
		"realtime_context_len": len(realtimeContext),
		"realtime_injected":    realtimeInjected,
	})
	return messages, registry
}

// trimAPIHybridKnowledgeContext preserves realtime evidence and response room
// for the 128K chat model window. Rune counting is deliberately conservative
// for Chinese, where a rune is often close to one token.
func trimAPIHybridKnowledgeContext(
	ctx context.Context, chatManage *types.ChatManage, ordered []*types.SearchResult,
) []*types.SearchResult {
	if chatManage == nil || len(ordered) == 0 {
		return ordered
	}
	used := utf8.RuneCountInString(chatManage.SummaryConfig.Prompt) +
		utf8.RuneCountInString(chatManage.Query) +
		utf8.RuneCountInString(chatManage.RealtimeContext)
	for _, history := range chatManage.History {
		if history != nil {
			used += utf8.RuneCountInString(history.Query) + utf8.RuneCountInString(history.Answer)
		}
	}
	limited := make([]*types.SearchResult, 0, len(ordered))
	for _, result := range ordered {
		if result == nil {
			continue
		}
		passage := stripAPIHybridSourceArtifacts(getEnrichedPassageForChat(ctx, result))
		if used+utf8.RuneCountInString(passage) > apiHybridContextRuneBudget {
			continue
		}
		limited = append(limited, result)
		used += utf8.RuneCountInString(passage)
	}
	if len(limited) != len(ordered) {
		pipelineInfo(ctx, "ModelContext", "api_hybrid_trim", map[string]interface{}{
			"before":      len(ordered),
			"after":       len(limited),
			"rune_budget": apiHybridContextRuneBudget,
			"used_runes":  used,
		})
	}
	return limited
}

func stripAPIHybridSourceArtifacts(content string) string {
	content = searchutil.MarkdownImageRegex.ReplaceAllString(content, "")
	return strings.TrimSpace(content)
}

func renderAPIHybridModelContext(knowledgeRows, webRows []map[string]interface{}) string {
	var out strings.Builder
	if len(knowledgeRows) > 0 {
		out.WriteString("<retrieved_knowledge>\n")
		for _, row := range knowledgeRows {
			if content := strings.TrimSpace(apiHybridRowString(row, "content")); content != "" {
				out.WriteString("  <item>")
				out.WriteString(html.EscapeString(content))
				out.WriteString("</item>\n")
			}
		}
		out.WriteString("</retrieved_knowledge>")
	}
	if len(webRows) > 0 {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString("<retrieved_web>\n")
		for _, row := range webRows {
			if content := strings.TrimSpace(apiHybridRowString(row, "snippet")); content != "" {
				out.WriteString("  <item>")
				out.WriteString(html.EscapeString(content))
				out.WriteString("</item>\n")
			}
		}
		out.WriteString("</retrieved_web>")
	}
	return out.String()
}

func apiHybridRowString(row map[string]interface{}, key string) string {
	if row == nil {
		return ""
	}
	value, _ := row[key].(string)
	return value
}

func isPipelineWebReference(result *types.SearchResult) bool {
	if result == nil {
		return false
	}
	return strings.EqualFold(result.ChunkType, string(types.ChunkTypeWebSearch)) ||
		strings.EqualFold(result.KnowledgeSource, "web_search")
}

func orderedPipelineReferences(chatManage *types.ChatManage) []*types.SearchResult {
	if chatManage == nil {
		return nil
	}
	if !chatManage.FAQPriorityEnabled {
		return chatManage.MergeResult
	}
	ordered := make([]*types.SearchResult, 0, len(chatManage.MergeResult))
	for _, result := range chatManage.MergeResult {
		if result != nil && result.ChunkType == string(types.ChunkTypeFAQ) {
			ordered = append(ordered, result)
		}
	}
	for _, result := range chatManage.MergeResult {
		if result != nil && result.ChunkType != string(types.ChunkTypeFAQ) {
			ordered = append(ordered, result)
		}
	}
	return ordered
}

func firstPipelineTitle(result *types.SearchResult) string {
	if result == nil {
		return ""
	}
	if result.KnowledgeTitle != "" {
		return result.KnowledgeTitle
	}
	return result.KnowledgeFilename
}
