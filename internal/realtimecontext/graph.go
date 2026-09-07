package realtimecontext

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type graphResponse struct {
	Nodes       []graphNode `json:"nodes"`
	Edges       []graphEdge `json:"edges"`
	IsTruncated bool        `json:"is_truncated"`
}

type graphNode struct {
	ID         string         `json:"id"`
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties"`
}

type graphEdge struct {
	ID         string         `json:"id"`
	Source     string         `json:"source"`
	Target     string         `json:"target"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
}

type syncStatus struct {
	Success     bool   `json:"success"`
	Source      string `json:"source"`
	EntityCount int    `json:"entity_count"`
	Error       string `json:"error"`
}

type graphSnapshot struct {
	Nodes []graphNode
	Edges []graphEdge
}

func (r *resolver) refreshGraph(ctx context.Context) (graphSnapshot, error) {
	refreshCtx, cancel := context.WithTimeout(ctx, r.cfg.RequestTimeout)
	defer cancel()
	var status syncStatus
	if err := r.http.getJSON(refreshCtx, r.cfg.GraphBaseURL, "/api/sync/status", nil, &status); err != nil {
		return graphSnapshot{}, fmt.Errorf("read graph sync status: %w", err)
	}
	if !status.Success || !strings.EqualFold(status.Source, "platform") {
		return graphSnapshot{}, fmt.Errorf("graph snapshot is not a successful platform sync")
	}
	var graph graphResponse
	query := url.Values{"freshOnly": {"true"}, "limit": {"1000"}}
	if err := r.http.getJSON(refreshCtx, r.cfg.GraphBaseURL, "/api/graph", query, &graph); err != nil {
		return graphSnapshot{}, fmt.Errorf("read graph snapshot: %w", err)
	}
	if graph.IsTruncated {
		return graphSnapshot{}, fmt.Errorf("graph snapshot was truncated")
	}
	if status.EntityCount > 0 && len(graph.Nodes) == 0 {
		return graphSnapshot{}, fmt.Errorf("graph snapshot is unexpectedly empty")
	}
	return graphSnapshot{Nodes: graph.Nodes, Edges: graph.Edges}, nil
}

func matchEntities(snapshot graphSnapshot, request Request, limit int) []EntityEvidence {
	query := strings.TrimSpace(request.Query)
	if isFollowUpQuery(query) && len(request.History) > 0 {
		query += " " + request.History[len(request.History)-1].User
	}
	type scoredNode struct {
		node  graphNode
		score int
	}
	var matches []scoredNode
	requestedTypes := typesMentioned(query)
	groupQuery := hasEntityGroupOrLiveHint(query)
	for _, node := range snapshot.Nodes {
		if !isRealEntityNode(node) {
			continue
		}
		name := stringProperty(node.Properties, "name")
		id := firstNonEmpty(stringProperty(node.Properties, "entityId"), node.ID)
		specificType := strings.ToUpper(stringProperty(node.Properties, "specificType"))
		normalizedQuery := normalize(query)
		score := 0
		if id != "" && strings.Contains(normalizedQuery, normalize(id)) {
			score = 120
		}
		if name != "" && strings.Contains(normalizedQuery, normalize(name)) && runeLen(name) >= 2 {
			score = max(score, 110)
		}
		exactMatch := score >= 110
		if !exactMatch && !nodeMatchesRequestedTypes(node, requestedTypes, specificType) {
			continue
		}
		shared := longestCommonCJK(query, name)
		if len(requestedTypes) > 0 && shared >= 2 {
			score = max(score, 70+shared)
		} else if shared >= 3 && hasEntityFieldHint(query) {
			score = max(score, 50+shared)
		}
		if score == 0 && len(requestedTypes) > 0 && groupQuery {
			score = 20
		}
		if score == 0 {
			continue
		}
		if !matchesStatusFilter(query, node.Properties) {
			continue
		}
		matches = append(matches, scoredNode{node: node, score: score})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return stringProperty(matches[i].node.Properties, "name") < stringProperty(matches[j].node.Properties, "name")
	})
	// A query naming a concrete entity must not be diluted by same-type
	// candidates. Keep every exact ID/name hit (so multi-entity queries still
	// work), but drop broad type matches such as other Camera-labelled nodes.
	if len(matches) > 0 && matches[0].score >= 110 {
		exact := matches[:0]
		for _, match := range matches {
			if match.score >= 110 {
				exact = append(exact, match)
			}
		}
		matches = exact
	}
	if limit <= 0 {
		limit = 20
	}
	if len(matches) > limit {
		matches = matches[:limit]
	}
	byID := make(map[string]graphNode, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		byID[node.ID] = node
	}
	result := make([]EntityEvidence, 0, len(matches))
	for _, match := range matches {
		result = append(result, makeEntityEvidence(match.node, snapshot.Edges, byID))
	}
	return result
}

// nodeMatchesRequestedTypes accepts both canonical ontology types and the
// semantic labels/vendor-specific type names emitted by the entity graph. For
// example, a node labelled Camera with specificType ThirdPartyYuan8Camera is
// still a CAMERA for an "相机设备参数" question. Exact ID/name matches bypass
// this helper in matchEntities so vendor ontology changes never hide the
// explicitly requested entity.
func nodeMatchesRequestedTypes(node graphNode, requestedTypes map[string]bool, specificType string) bool {
	if len(requestedTypes) == 0 {
		return true
	}
	specific := normalize(specificType)
	for requested := range requestedTypes {
		canonical := normalize(requested)
		if canonical == "" {
			continue
		}
		if specific == canonical || strings.Contains(specific, canonical) {
			return true
		}
		for _, label := range node.Labels {
			semanticLabel := normalize(label)
			if semanticLabel == canonical || strings.Contains(semanticLabel, canonical) {
				return true
			}
		}
	}
	return false
}

func makeEntityEvidence(node graphNode, edges []graphEdge, byID map[string]graphNode) EntityEvidence {
	id := firstNonEmpty(stringProperty(node.Properties, "entityId"), node.ID)
	evidence := EntityEvidence{
		ID:         sanitizeExternalText(id),
		Name:       sanitizeExternalText(stringProperty(node.Properties, "name")),
		Type:       sanitizeExternalText(stringProperty(node.Properties, "specificType")),
		Properties: sanitizeEntityProperties(node.Properties),
	}
	for _, edge := range edges {
		var relatedID string
		if edge.Source == node.ID {
			relatedID = edge.Target
		} else if edge.Target == node.ID {
			relatedID = edge.Source
		} else {
			continue
		}
		related := byID[relatedID]
		evidence.Relationships = append(evidence.Relationships, RelationshipEvidence{
			Type: sanitizeExternalText(edge.Type),
			RelatedID: sanitizeExternalText(
				firstNonEmpty(stringProperty(related.Properties, "entityId"), relatedID),
			),
			RelatedName: sanitizeExternalText(stringProperty(related.Properties, "name")),
			RelatedType: sanitizeExternalText(stringProperty(related.Properties, "specificType")),
		})
	}
	return evidence
}

func isRealEntityNode(node graphNode) bool {
	if boolProperty(node.Properties, "virtual") || boolProperty(node.Properties, "displayOnly") {
		return false
	}
	return node.ID != "" && stringProperty(node.Properties, "name") != ""
}

var typeSynonyms = map[string][]string{
	"CAMERA":                 {"相机", "摄像机", "摄像头", "camera"},
	"RADAR":                  {"雷达", "radar"},
	"UAV":                    {"无人机", "uav"},
	"USV":                    {"无人船", "usv"},
	"DOCK":                   {"机巢", "机场", "停机坪", "dock"},
	"JAMMER":                 {"干扰机", "干扰设备", "jammer"},
	"LASER":                  {"激光", "laser"},
	"MUNITION":               {"巡飞弹", "弹药", "munition"},
	"TDOA":                   {"tdoa", "时差定位"},
	"PATROL_ROUTE":           {"巡逻航线", "航线", "route"},
	"SURVEILLANCE_AREA":      {"监视区域", "监控区域"},
	"VISION_DETECTION_MODEL": {"视觉检测模型", "检测模型"},
}

func typesMentioned(query string) map[string]bool {
	query = normalize(query)
	result := make(map[string]bool)
	for specificType, synonyms := range typeSynonyms {
		for _, synonym := range synonyms {
			if strings.Contains(query, normalize(synonym)) {
				result[specificType] = true
				break
			}
		}
	}
	return result
}

func shouldConsiderEntityQuery(query string) bool {
	if hasConceptualOnlyHint(query) {
		return false
	}
	return len(typesMentioned(query)) > 0 && hasEntityFieldHint(query) || looksLikeEntityID(query)
}

func hasEntityFieldHint(query string) bool {
	return containsAny(normalize(query), []string{
		"当前", "现在", "实时", "最新", "设备参数", "参数", "状态", "在线", "离线", "健康",
		"位置", "坐标", "性能", "型号", "分辨率", "帧率", "可见距离", "任务", "能力", "有哪些", "多少",
	})
}

func hasEntityGroupOrLiveHint(query string) bool {
	return hasEntityFieldHint(query) || containsAny(normalize(query), []string{"列表", "全部", "所有"})
}

func hasConceptualOnlyHint(query string) bool {
	normalized := normalize(query)
	return containsAny(normalized, []string{"原理", "工作原理", "概念", "教程", "如何实现", "怎么实现", "知识", "文档"}) &&
		!containsAny(normalized, []string{"当前", "现在", "实时", "最新", "在线", "离线"})
}

func isFollowUpQuery(query string) bool {
	return containsAny(normalize(query), []string{"它", "这个", "该设备", "上一个", "刚才", "其中"})
}

func matchesStatusFilter(query string, properties map[string]any) bool {
	normalized := normalize(query)
	status := strings.ToUpper(stringProperty(properties, "healthStatus") + " " + stringProperty(properties, "sensorStatus"))
	online := boolProperty(properties, "isOnline") || boolProperty(properties, "isLive")
	if strings.Contains(normalized, "离线") {
		return !online || strings.Contains(status, "OFFLINE")
	}
	if strings.Contains(normalized, "在线") && !strings.Contains(normalized, "离线") {
		return online && !strings.Contains(status, "OFFLINE")
	}
	return true
}
