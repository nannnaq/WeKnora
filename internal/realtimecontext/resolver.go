package realtimecontext

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/models/chat"
)

const (
	defaultCacheTTL       = time.Minute
	defaultRequestTimeout = 5 * time.Second
)

type Option func(*resolver)

// WithHTTPClient is intended for tests and controlled embedding environments.
// Production construction uses WeKnora's SSRF-safe HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(r *resolver) {
		r.http = newHTTPReader(r.cfg.RequestTimeout, client)
		r.disableWarmup = true
	}
}

type resolver struct {
	cfg           config.RealtimeContextConfig
	http          *httpReader
	models        ChatModelProvider
	graphCache    cachedSource[graphSnapshot]
	ruleCache     cachedSource[ruleSnapshot]
	disableWarmup bool
}

type ChatModelProvider interface {
	GetChatModel(ctx context.Context, modelID string) (chat.Chat, error)
}

func NewResolver(cfg config.RealtimeContextConfig, models ChatModelProvider, options ...Option) Resolver {
	applyConfigDefaults(&cfg)
	r := &resolver{
		cfg:    cfg,
		http:   newHTTPReader(cfg.RequestTimeout, nil),
		models: models,
	}
	for _, option := range options {
		option(r)
	}
	if cfg.Enabled && !r.disableWarmup {
		go r.warmup()
	}
	return r
}

func (r *resolver) warmup() {
	ctx, cancel := context.WithTimeout(context.Background(), r.cfg.RequestTimeout)
	defer cancel()
	if strings.TrimSpace(r.cfg.GraphBaseURL) != "" {
		_, _, _, _ = r.graphCache.load(ctx, r.cfg.CacheTTL, r.refreshGraph)
	}
	if strings.TrimSpace(r.cfg.RuleBaseURL) != "" {
		_, _, _, _ = r.ruleCache.load(ctx, r.cfg.CacheTTL, r.refreshRules)
	}
}

func applyConfigDefaults(cfg *config.RealtimeContextConfig) {
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = defaultCacheTTL
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.MaxMatchedEntities <= 0 {
		cfg.MaxMatchedEntities = 20
	}
	if cfg.MaxContextChars <= 0 {
		cfg.MaxContextChars = 32_000
	}
}

func (r *resolver) Resolve(ctx context.Context, request Request) Bundle {
	bundle := Bundle{MaxChars: r.cfg.MaxContextChars}
	if !r.cfg.Enabled || strings.TrimSpace(request.Query) == "" {
		return bundle
	}

	if shouldConsiderRuleQuery(request.Query) {
		bundle.Attempted = true
		r.resolveRules(ctx, request, &bundle)
	}

	if shouldConsiderEntityQuery(request.Query) || r.cachedGraphMatches(request) {
		graph, freshness, asOf, err := r.graphCache.load(ctx, r.cfg.CacheTTL, r.refreshGraph)
		entities := matchEntities(graph, request, r.cfg.MaxMatchedEntities)
		entities = r.selectAmbiguousEntities(ctx, request, entities)
		if len(entities) > 0 || freshness == FreshnessUnavailable {
			bundle.Attempted = true
			bundle.Entities = entities
			status := SourceStatus{Name: "entity_management_graph", Freshness: freshness, AsOf: asOf}
			if err != nil {
				status.Message = sourceFailureMessage(freshness)
			}
			bundle.Sources = append(bundle.Sources, status)
		}
	}

	return bundle
}

func (r *resolver) cachedGraphMatches(request Request) bool {
	graph, _, ok := r.graphCache.peek()
	return ok && len(matchEntities(graph, request, 1)) > 0
}

func sourceFailureMessage(freshness Freshness) string {
	if freshness == FreshnessStale {
		return "实时刷新失败；以下内容仅为本进程最后一次成功快照，不能视为当前值"
	}
	return "实时来源当前不可用，无法确认当前值"
}
