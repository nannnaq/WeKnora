package agent

import (
	"context"
	"github.com/Tencent/WeKnora/internal/realtimecontext"
	"strings"
)

func (e *AgentEngine) buildSystemPrompt(ctx context.Context) string {
	prompt := e.buildBaseSystemPrompt(ctx)
	if e.config != nil && e.config.RealtimeAttempted {
		prompt = strings.TrimRight(prompt, " \t\r\n") + "\n\n## Live evidence handling\n" + realtimecontext.AnswerInstruction
	}
	return prompt
}

func appendRealtimeEvidence(runtimeContext, evidence string) string {
	if strings.TrimSpace(evidence) == "" {
		return runtimeContext
	}
	closing := "</runtime_context>"
	index := strings.LastIndex(runtimeContext, closing)
	if index < 0 {
		return runtimeContext + "\n" + evidence
	}
	var out strings.Builder
	out.WriteString(runtimeContext[:index])
	out.WriteString("  <live_external_evidence>\n")
	out.WriteString(indentLines(evidence, "    "))
	out.WriteString("\n  </live_external_evidence>\n")
	out.WriteString(runtimeContext[index:])
	return out.String()
}
