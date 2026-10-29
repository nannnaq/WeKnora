package externalidentity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupWeb(t *testing.T) fixture {
	f := setup(t)
	f.source.Web = &WebConfig{Origin: "https://weknora.test", ParentOrigins: []string{"https://dsh.test"}}
	f.s.sources[f.source.ID] = f.source
	return f
}

func TestWebSessionIdentityReplayAndRevoke(t *testing.T) {
	f := setupWeb(t)
	ctx := context.Background()
	alice, err := f.s.apply(ctx, f.source, request("alice", 1), "ensure")
	require.NoError(t, err)
	launch, err := f.s.launchWeb(ctx, f.source, WebRequest{Request: request("alice", 1), ParentOrigin: "https://dsh.test"})
	require.NoError(t, err)
	require.NotContains(t, launch.URL, launch.Ticket)
	require.NotContains(t, launch.URL, alice.APIKey)
	_, err = f.s.exchangeWeb(ctx, f.source, launch.Ticket, "https://evil.test")
	require.Error(t, err)
	access, err := f.s.exchangeWeb(ctx, f.source, launch.Ticket, "https://dsh.test")
	require.NoError(t, err)
	user, tenant, err := f.s.users.ValidateToken(ctx, access)
	require.NoError(t, err)
	require.Equal(t, alice.UserID, user.ID)
	require.Equal(t, alice.TenantID, tenant)
	require.False(t, user.IsSystemAdmin)
	_, err = f.s.exchangeWeb(ctx, f.source, launch.Ticket, "https://dsh.test")
	require.Error(t, err)
	var refreshCount int64
	require.NoError(t, f.db.Model(&types.AuthToken{}).Where("user_id = ? AND token_type = ? AND is_revoked = ?", user.ID, "refresh_token", false).Count(&refreshCount).Error)
	require.Zero(t, refreshCount)
	// Revocation affects integration-issued sessions, preserving standalone login.
	ordinary, _, err := f.s.users.GenerateTokens(ctx, user)
	require.NoError(t, err)
	pending, err := f.s.launchWeb(ctx, f.source, WebRequest{Request: request("alice", 1), ParentOrigin: "https://dsh.test"})
	require.NoError(t, err)
	_, err = f.s.apply(ctx, f.source, request("alice", 2), "revoke")
	require.NoError(t, err)
	_, _, err = f.s.users.ValidateToken(ctx, access)
	require.Error(t, err)
	_, _, err = f.s.users.ValidateToken(ctx, ordinary)
	require.NoError(t, err)
	_, err = f.s.exchangeWeb(ctx, f.source, pending.Ticket, "https://dsh.test")
	require.Error(t, err)
}

func TestWebTicketExpiryConcurrentExchangeAndRevision(t *testing.T) {
	f := setupWeb(t)
	ctx := context.Background()
	_, err := f.s.apply(ctx, f.source, request("alice", 1), "ensure")
	require.NoError(t, err)
	issue := func() *WebLaunch {
		launch, err := f.s.launchWeb(ctx, f.source, WebRequest{Request: request("alice", 1), ParentOrigin: "https://dsh.test"})
		require.NoError(t, err)
		return launch
	}
	expired := issue()
	require.NoError(t, f.db.Model(&WebSession{}).Where("digest = ?", ticketDigest(expired.Ticket)).Update("expires_at", time.Now().Add(-time.Minute)).Error)
	_, err = f.s.exchangeWeb(ctx, f.source, expired.Ticket, "https://dsh.test")
	require.Error(t, err)
	first, second := issue(), issue()
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.s.exchangeWeb(ctx, f.source, first.Ticket, "https://dsh.test")
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, 1, successes)
	_, err = f.s.exchangeWeb(ctx, f.source, second.Ticket, "https://dsh.test")
	require.NoError(t, err)
	stale := issue()
	_, err = f.s.apply(ctx, f.source, request("alice", 2), "revoke")
	require.NoError(t, err)
	_, err = f.s.apply(ctx, f.source, request("alice", 3), "ensure")
	require.NoError(t, err)
	_, err = f.s.exchangeWeb(ctx, f.source, stale.Ticket, "https://dsh.test")
	require.Error(t, err)
}

func TestWebHTTPTrustBoundaries(t *testing.T) {
	f := setupWeb(t)
	r := gin.New()
	f.s.Register(r)
	for _, origin := range []string{"", "https://evil.test"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/external-app/dsh-hub/exchange", strings.NewReader(`{"ticket":"fake","parent_origin":"https://dsh.test"}`))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, 403, rec.Code)
	}
	for _, test := range []struct {
		path   string
		status int
	}{{"/external-app/dsh-hub/platform/agents", 200}, {"/external-app/unknown/platform/agents", 403}, {"/platform/agents", 403}} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations/external-app/frame-policy", nil)
		req.Header.Set("X-External-App-URI", test.path)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, test.status, rec.Code)
		require.NotContains(t, rec.Body.String(), "fixture-provisioner")
		if test.status == 200 {
			require.Equal(t, "frame-ancestors https://dsh.test", rec.Header().Get("Content-Security-Policy"))
		}
	}
	for _, origin := range []string{"https://good.test/path", "https://good.test;frame-ancestors *", "null", "https://user:password@good.test", "https://good.test?x=1"} {
		require.False(t, webOrigin(origin))
	}
}
