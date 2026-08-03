package realtimecontext

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

type ruleSetRead struct {
	DBID      string  `json:"db_id"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Version   string  `json:"version"`
	Status    string  `json:"status"`
	DeletedAt *string `json:"deleted_at"`
}

type ruleSnapshot struct {
	RuleSets []RuleSetEvidence
	Partial  bool
}

func shouldConsiderRuleQuery(query string) bool {
	normalized := strings.ToLower(strings.TrimSpace(query))
	return containsAny(normalized, []string{
		"交战规则", "交战策略", "已生效规则", "生效规则", "规则集", "硬规则", "软规则", "防御圈",
		"处置规则", "cancan", "hr_", "sr_",
	})
}

func (r *resolver) refreshRules(ctx context.Context) (ruleSnapshot, error) {
	refreshCtx, cancel := context.WithTimeout(ctx, r.cfg.RequestTimeout)
	defer cancel()
	var all []ruleSetRead
	if err := r.http.getJSON(refreshCtx, r.cfg.RuleBaseURL, "/api/v1/rule-base-rule-sets", nil, &all); err != nil {
		return ruleSnapshot{}, fmt.Errorf("read rule set list: %w", err)
	}
	enabled := make([]ruleSetRead, 0, len(all))
	for _, item := range all {
		if strings.EqualFold(strings.TrimSpace(item.Status), "enabled") && item.DeletedAt == nil {
			enabled = append(enabled, item)
		}
	}

	snapshot := ruleSnapshot{RuleSets: make([]RuleSetEvidence, len(enabled))}
	var wg sync.WaitGroup
	var mu sync.Mutex
	for index, item := range enabled {
		index, item := index, item
		wg.Add(1)
		go func() {
			defer wg.Done()
			evidence := RuleSetEvidence{
				DBID: item.DBID,
				Code: sanitizeExternalText(item.Code), Name: sanitizeExternalText(item.Name),
				Version: sanitizeExternalText(item.Version),
			}
			var ruleout map[string]any
			path := "/api/v1/rule-base-rule-sets/" + url.PathEscape(item.DBID) + "/rule-base-ruleout.json"
			if err := r.http.getJSON(refreshCtx, r.cfg.RuleBaseURL, path, nil, &ruleout); err != nil {
				evidence.Error = "当前已生效规则内容读取失败"
				mu.Lock()
				snapshot.Partial = true
				mu.Unlock()
			} else {
				evidence.Data = sanitizeRuleout(ruleout)
			}
			snapshot.RuleSets[index] = evidence
		}()
	}
	wg.Wait()
	return snapshot, nil
}

func sanitizeRuleout(ruleout map[string]any) map[string]any {
	allowed := map[string]bool{
		"version": true, "source": true, "global_parameters": true, "cancan_matrix": true,
		"capabilities": true, "hard_rules": true, "soft_rules": true,
	}
	clean := make(map[string]any)
	for key, value := range ruleout {
		if !allowed[key] {
			continue
		}
		if sanitized, ok := sanitizeValue(key, value); ok {
			clean[key] = sanitized
		}
	}
	return clean
}

func (r *resolver) resolveRules(ctx context.Context, request Request, bundle *Bundle) {
	snapshot, freshness, asOf, err := r.ruleCache.load(ctx, r.cfg.CacheTTL, r.refreshRules)
	status := SourceStatus{Name: "engagement_rule_system", Freshness: freshness, AsOf: asOf}
	if err != nil {
		status.Message = sourceFailureMessage(freshness)
	} else if snapshot.Partial {
		status.Message = "部分已启用规则集读取失败；已成功读取的规则仍可使用"
	}
	bundle.Sources = append(bundle.Sources, status)
	if freshness == FreshnessUnavailable {
		return
	}

	selected := selectRuleSets(snapshot.RuleSets, request.Query)
	bundle.RuleSets = append(bundle.RuleSets, selected...)
}

func selectRuleSets(sets []RuleSetEvidence, query string) []RuleSetEvidence {
	normalized := normalize(query)
	var selected []RuleSetEvidence
	for _, set := range sets {
		if containsNamedRuleSet(normalized, set) || ruleoutContainsIdentifier(set.Data, query) {
			selected = append(selected, set)
		}
	}
	if len(selected) > 0 {
		return selected
	}
	return append([]RuleSetEvidence(nil), sets...)
}

func containsNamedRuleSet(normalizedQuery string, set RuleSetEvidence) bool {
	for _, candidate := range []string{set.Code, set.Name} {
		candidate = normalize(candidate)
		if runeLen(candidate) >= 2 && strings.Contains(normalizedQuery, candidate) {
			return true
		}
	}
	return false
}

func ruleoutContainsIdentifier(data map[string]any, query string) bool {
	if len(data) == 0 {
		return false
	}
	upper := strings.ToUpper(query)
	if !strings.Contains(upper, "HR_") && !strings.Contains(upper, "SR_") {
		return false
	}
	raw, err := json.Marshal(data)
	return err == nil && strings.Contains(strings.ToUpper(string(raw)), upperRuleIdentifier(upper))
}

func upperRuleIdentifier(query string) string {
	for _, prefix := range []string{"HR_", "SR_"} {
		if index := strings.Index(query, prefix); index >= 0 {
			end := index + len(prefix)
			for end < len(query) && query[end] >= '0' && query[end] <= '9' {
				end++
			}
			return query[index:end]
		}
	}
	return ""
}
