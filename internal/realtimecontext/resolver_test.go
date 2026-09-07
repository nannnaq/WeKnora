package realtimecontext

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	modelchat "github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type selectorModelProvider struct {
	model modelchat.Chat
}

func (p selectorModelProvider) GetChatModel(context.Context, string) (modelchat.Chat, error) {
	return p.model, nil
}

type selectorChatModel struct {
	calls atomic.Int32
}

func (m *selectorChatModel) Chat(context.Context, []modelchat.Message, *modelchat.ChatOptions) (*types.ChatResponse, error) {
	m.calls.Add(1)
	return &types.ChatResponse{Content: `{"entity_ids":["camera_2"]}`}, nil
}

func (m *selectorChatModel) ChatStream(context.Context, []modelchat.Message, *modelchat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}

func (m *selectorChatModel) GetModelName() string { return "selector" }
func (m *selectorChatModel) GetModelID() string   { return "selector-1" }

func TestResolverAddsMatchingEntityAndRemovesSensitiveFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sync/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"source":"platform","entity_count":2}`))
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"nodes":[{
				"id":"camera_004",
				"labels":["Entity","Sensor","Camera"],
				"properties":{
					"entityId":"camera_004",
					"name":"远遥码头-可见光",
					"specificType":"CAMERA",
					"healthStatus":"HEALTH_STATUS_HEALTHY",
					"isOnline":1,
					"fps":25,
					"resolutionWidth":1920,
					"resolutionHeight":1080,
					"sensorCapabilitiesJson":"{\"visibility\":3146.32,\"password\":\"nested-secret\"}",
					"streamAddress":"webrtc://192.168.18.141/live/camera_004",
					"rawJson":"{\"aliases\":{\"auth\":{\"username\":\"admin\",\"password\":\"secret\"}}}"
				}
			},{
				"id":"controller_1",
				"properties":{"entityId":"controller_1","name":"控制器-192.168.18.141","specificType":"CONTROLLER"}
			}],
			"edges":[{"id":"edge_1","source":"camera_004","target":"controller_1","type":"CONTROLLED_BY","properties":{}}]
		}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled:            true,
		GraphBaseURL:       server.URL,
		RuleBaseURL:        server.URL,
		CacheTTL:           time.Minute,
		RequestTimeout:     time.Second,
		MaxMatchedEntities: 20,
		MaxContextChars:    32_000,
	}, nil, WithHTTPClient(server.Client()))

	bundle := resolver.Resolve(context.Background(), Request{Query: "远遥相机的设备参数是什么？"})
	rendered := bundle.Render()

	require.True(t, bundle.Attempted)
	require.True(t, bundle.Usable())
	require.Contains(t, rendered, "远遥码头-可见光")
	require.Contains(t, rendered, "resolutionWidth")
	require.Contains(t, rendered, "3146.32")
	for _, forbidden := range []string{"streamAddress", "webrtc://", "192.168.18.141", "rawJson", "username", "password", "secret"} {
		require.NotContains(t, strings.ToLower(rendered), strings.ToLower(forbidden), fmt.Sprintf("must remove %s", forbidden))
	}
}

func TestResolverMatchesVendorCameraTypeUsingGraphLabel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sync/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"source":"platform","entity_count":1}`))
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"nodes":[{
				"id":"camera-cs-001",
				"labels":["Entity","Sensor","Camera"],
				"properties":{
					"entityId":"camera-cs-001",
					"name":"1号相机",
					"specificType":"ThirdPartyYuan8Camera",
					"isOnline":1,
					"healthStatus":"HEALTH_STATUS_HEALTHY",
					"resolutionWidth":1920,
					"resolutionHeight":1080,
					"fps":25,
					"visibility":6000
				}
			}],"edges":[]
		}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled: true, GraphBaseURL: server.URL, CacheTTL: time.Minute, RequestTimeout: time.Second,
	}, nil, WithHTTPClient(server.Client()))

	bundle := resolver.Resolve(context.Background(), Request{Query: "1号相机的设备参数是什么？"})

	require.True(t, bundle.Attempted)
	require.True(t, bundle.Usable())
	require.Len(t, bundle.Entities, 1)
	require.Equal(t, "camera-cs-001", bundle.Entities[0].ID)
	require.Equal(t, "1号相机", bundle.Entities[0].Name)
	require.Equal(t, float64(1920), bundle.Entities[0].Properties["resolutionWidth"])
}

func TestMatchEntitiesMatchesVendorTypesAndPreservesExactNameMatch(t *testing.T) {
	snapshot := graphSnapshot{Nodes: []graphNode{
		{
			ID:     "radar-vendor-001",
			Labels: []string{"Entity", "Radar"},
			Properties: map[string]any{
				"entityId": "radar-vendor-001", "name": "近程雷达", "specificType": "VendorShortRangeRadar",
			},
		},
		{
			ID: "camera-vendor-001",
			Properties: map[string]any{
				"entityId": "camera-vendor-001", "name": "甲设备", "specificType": "VendorOpticalPayload",
			},
		},
	}}

	radar := matchEntities(snapshot, Request{Query: "雷达设备参数"}, 20)
	exactName := matchEntities(snapshot, Request{Query: "甲设备相机的设备参数"}, 20)

	require.Len(t, radar, 1)
	require.Equal(t, "radar-vendor-001", radar[0].ID)
	require.Len(t, exactName, 1)
	require.Equal(t, "camera-vendor-001", exactName[0].ID)
}

func TestResolverReturnsEveryEnabledRuleSetForGenericCurrentRulesQuestion(t *testing.T) {
	var disabledRuleReads int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/rule-base-rule-sets", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"db_id":"db-a","code":"default","name":"默认决策交战规则","version":"260620","status":"enabled"},
			{"db_id":"db-b","code":"harbor","name":"港口交战规则","version":"2","status":"enabled"},
			{"db_id":"db-c","code":"old","name":"旧规则","version":"1","status":"disabled"}
		]`))
	})
	mux.HandleFunc("/api/v1/rule-base-rule-sets/db-a/rule-base-ruleout.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"260620","hard_rules":[{"id":"HR_001","name":"速度劣势否决"}]}`))
	})
	mux.HandleFunc("/api/v1/rule-base-rule-sets/db-b/rule-base-ruleout.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"2","soft_rules":[{"id":"SR_001","name":"外层优先处置"}]}`))
	})
	mux.HandleFunc("/api/v1/rule-base-rule-sets/db-c/rule-base-ruleout.json", func(w http.ResponseWriter, _ *http.Request) {
		disabledRuleReads++
		_, _ = w.Write([]byte(`{}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled:        true,
		GraphBaseURL:   server.URL,
		RuleBaseURL:    server.URL,
		CacheTTL:       time.Minute,
		RequestTimeout: time.Second,
	}, nil, WithHTTPClient(server.Client()))

	bundle := resolver.Resolve(context.Background(), Request{Query: "现在的交战规则是什么？"})
	rendered := bundle.Render()

	require.True(t, bundle.Attempted)
	require.Len(t, bundle.RuleSets, 2)
	require.Contains(t, rendered, "default")
	require.Contains(t, rendered, "harbor")
	require.NotContains(t, rendered, "旧规则")
	require.Zero(t, disabledRuleReads)
}

func TestResolverReturnsOnlyNamedEnabledRuleSet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/rule-base-rule-sets", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"db_id":"db-a","code":"default","name":"默认决策交战规则","version":"260620","status":"enabled"},
			{"db_id":"db-b","code":"harbor","name":"港口交战规则","version":"2","status":"enabled"}
		]`))
	})
	mux.HandleFunc("/api/v1/rule-base-rule-sets/db-a/rule-base-ruleout.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"hard_rules":[{"id":"HR_001"}]}`))
	})
	mux.HandleFunc("/api/v1/rule-base-rule-sets/db-b/rule-base-ruleout.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"hard_rules":[{"id":"HR_002"}]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled: true, RuleBaseURL: server.URL, CacheTTL: time.Minute, RequestTimeout: time.Second,
	}, nil, WithHTTPClient(server.Client()))
	bundle := resolver.Resolve(context.Background(), Request{Query: "港口交战规则现在是什么？"})

	require.Len(t, bundle.RuleSets, 1)
	require.Equal(t, "harbor", bundle.RuleSets[0].Code)
	require.NotContains(t, bundle.Render(), "default")
}

func TestResolverUsesLastSuccessfulSnapshotWhenRefreshFails(t *testing.T) {
	var statusCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sync/status", func(w http.ResponseWriter, _ *http.Request) {
		if statusCalls.Add(1) > 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"source":"platform","entity_count":1}`))
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"nodes":[{"id":"camera_004","properties":{"entityId":"camera_004","name":"远遥码头-可见光","specificType":"CAMERA","fps":25}}],"edges":[]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled: true, GraphBaseURL: server.URL, CacheTTL: time.Millisecond, RequestTimeout: time.Second,
	}, nil, WithHTTPClient(server.Client()))
	request := Request{Query: "远遥相机当前参数"}
	first := resolver.Resolve(context.Background(), request)
	require.Equal(t, FreshnessFresh, first.Sources[0].Freshness)
	time.Sleep(5 * time.Millisecond)

	second := resolver.Resolve(context.Background(), request)

	require.True(t, second.Usable())
	require.Equal(t, FreshnessStale, second.Sources[0].Freshness)
	require.Equal(t, first.Sources[0].AsOf, second.Sources[0].AsOf)
	require.Contains(t, second.Render(), "最后一次成功快照")
}

func TestResolverDoesNotFetchLiveDataForGenericConceptQuestion(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled: true, GraphBaseURL: server.URL, RuleBaseURL: server.URL,
	}, nil, WithHTTPClient(server.Client()))

	bundle := resolver.Resolve(context.Background(), Request{Query: "相机的工作原理是什么？"})

	require.False(t, bundle.Attempted)
	require.Empty(t, bundle.Render())
	require.Zero(t, requests.Load())
}

func TestResolverRejectsSampleGraphAsCurrentPlatformEvidence(t *testing.T) {
	var graphReads atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sync/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"source":"sample","entity_count":1}`))
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, _ *http.Request) {
		graphReads.Add(1)
		_, _ = w.Write([]byte(`{"nodes":[],"edges":[]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled: true, GraphBaseURL: server.URL, RequestTimeout: time.Second,
	}, nil, WithHTTPClient(server.Client()))

	bundle := resolver.Resolve(context.Background(), Request{Query: "远遥相机当前参数"})

	require.True(t, bundle.Attempted)
	require.False(t, bundle.Usable())
	require.Equal(t, FreshnessUnavailable, bundle.Sources[0].Freshness)
	require.Contains(t, bundle.Render(), "无法确认当前值")
	require.Zero(t, graphReads.Load())
}

func TestResolverCoalescesConcurrentGraphRefreshes(t *testing.T) {
	var statusReads atomic.Int32
	var graphReads atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sync/status", func(w http.ResponseWriter, _ *http.Request) {
		statusReads.Add(1)
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte(`{"success":true,"source":"platform","entity_count":1}`))
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, _ *http.Request) {
		graphReads.Add(1)
		_, _ = w.Write([]byte(`{"nodes":[{"id":"camera_004","properties":{"name":"远遥相机","specificType":"CAMERA"}}],"edges":[]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled: true, GraphBaseURL: server.URL, CacheTTL: time.Minute, RequestTimeout: time.Second,
	}, nil, WithHTTPClient(server.Client()))

	var wg sync.WaitGroup
	results := make(chan bool, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bundle := resolver.Resolve(context.Background(), Request{Query: "远遥相机当前参数"})
			results <- bundle.Usable()
		}()
	}
	wg.Wait()
	close(results)
	for usable := range results {
		require.True(t, usable)
	}

	require.EqualValues(t, 1, statusReads.Load())
	require.EqualValues(t, 1, graphReads.Load())
}

func TestResolverUsesCurrentQAModelOnlyForAmbiguousFollowUp(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sync/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"source":"platform","entity_count":2}`))
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"nodes":[
			{"id":"camera_1","properties":{"name":"远遥码头-可见光","specificType":"CAMERA","fps":25}},
			{"id":"camera_2","properties":{"name":"远遥码头-红外","specificType":"CAMERA","fps":30}}
		],"edges":[]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	model := &selectorChatModel{}
	resolver := NewResolver(config.RealtimeContextConfig{
		Enabled: true, GraphBaseURL: server.URL, CacheTTL: time.Minute, RequestTimeout: time.Second,
	}, selectorModelProvider{model: model}, WithHTTPClient(server.Client()))
	// Populate the safe entity catalog without requiring the model.
	first := resolver.Resolve(context.Background(), Request{Query: "远遥相机当前参数"})
	require.Len(t, first.Entities, 2)

	followUp := resolver.Resolve(context.Background(), Request{
		Query:   "它现在的参数呢？",
		ModelID: "qa-model-1",
		History: []Turn{{User: "对比远遥相机", Assistant: "有可见光和红外两台"}},
	})

	require.Len(t, followUp.Entities, 1)
	require.Equal(t, "camera_2", followUp.Entities[0].ID)
	require.EqualValues(t, 1, model.calls.Load())
}

func TestRenderedRealtimeEvidenceKeepsClosedEnvelopeWhenTruncated(t *testing.T) {
	bundle := Bundle{
		Attempted: true,
		Entities: []EntityEvidence{{
			ID: "camera_004", Name: strings.Repeat("远遥相机", 200), Type: "CAMERA",
		}},
		MaxChars: 600,
	}

	rendered := bundle.Render()

	require.LessOrEqual(t, len(rendered), bundle.MaxChars)
	require.Contains(t, rendered, "实时上下文因长度限制已截断")
	require.True(t, strings.HasSuffix(rendered, "</realtime_evidence>"))
}
