package externalidentity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fixture struct {
	s      *Service
	db     *gorm.DB
	source SourceConfig
	org    *types.Organization
	shares interfaces.KBShareService
}

func setup(t *testing.T) fixture {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "bridge.db")+"?_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return setupDatabase(t, db)
}

func setupDatabase(t *testing.T, db *gorm.DB) fixture {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&types.Tenant{}, &types.User{}, &types.AuthToken{}, &types.TenantMember{}, &types.Organization{}, &types.OrganizationTenantMember{}, &types.TenantAPIKey{}, &types.KnowledgeBase{}, &types.KnowledgeBaseShare{}))
	usersRepo := repository.NewUserRepository(db)
	tenantService := service.NewTenantService(repository.NewTenantRepository(db), nil)
	members := service.NewTenantMemberService(repository.NewTenantMemberRepository(db), nil, usersRepo, nil)
	users := service.NewUserService(&config.Config{}, usersRepo, nil, tenantService, members, nil)
	orgRepo := repository.NewOrganizationRepository(db)
	shareRepo := repository.NewKBShareRepository(db)
	orgs := service.NewOrganizationService(orgRepo, usersRepo, shareRepo, nil)
	keys := service.NewTenantAPIKeyService(repository.NewTenantAPIKeyRepository(db))
	ctx := context.Background()
	owner, err := users.Register(ctx, &types.RegisterRequest{Username: "space-owner", Email: "owner@example.test", Password: "fixture-password", TenantProvisioning: types.TenantProvisioningCreatePersonal})
	require.NoError(t, err)
	zero := 0
	org, err := orgs.CreateOrganization(ctx, owner.ID, owner.TenantID, &types.CreateOrganizationRequest{Name: "团队知识库", InviteCodeValidityDays: &zero, MemberLimit: &zero})
	require.NoError(t, err)
	source := SourceConfig{ID: "dsh-hub", Issuer: "fixture-platform", InviteCode: org.InviteCode, Role: types.OrgRoleEditor, tokenHash: sha256.Sum256([]byte("fixture-provisioner-credential-123456789"))}
	s, err := newService(db, users, orgs, keys, &Config{Sources: []SourceConfig{source}})
	require.NoError(t, err)
	shares := service.NewKBShareService(shareRepo, orgRepo, repository.NewKnowledgeBaseRepository(db), nil, nil, nil)
	return fixture{s, db, source, org, shares}
}

func request(subject string, revision uint64) Request {
	return Request{Issuer: "fixture-platform", Subject: subject, Username: subject, Revision: revision}
}

func TestAccountsHavePersonalOwnershipAndBoundedSharedEditing(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	alice, err := f.s.apply(ctx, f.source, request("alice", 1), "ensure")
	require.NoError(t, err)
	bob, err := f.s.apply(ctx, f.source, request("bob", 1), "ensure")
	require.NoError(t, err)
	require.NotEqual(t, alice.UserID, bob.UserID)
	require.NotEqual(t, alice.TenantID, bob.TenantID)
	require.NotEqual(t, alice.APIKey, bob.APIKey)
	for _, result := range []*Result{alice, bob} {
		user, err := f.s.users.GetUserByID(ctx, result.UserID)
		require.NoError(t, err)
		require.False(t, user.IsSystemAdmin)
		require.False(t, user.CanAccessAllTenants)
		var member types.TenantMember
		require.NoError(t, f.db.Where("user_id = ? AND tenant_id = ?", result.UserID, result.TenantID).First(&member).Error)
		require.Equal(t, types.TenantRoleOwner, member.Role)
		key, err := f.s.keys.AuthenticateAPIKey(ctx, result.APIKey)
		require.NoError(t, err)
		require.False(t, key.IsPlatform())
		require.True(t, key.FullAccess)
		require.Equal(t, result.TenantID, key.TenantIDValue())
		_, denied := f.s.orgs.UpdateOrganization(ctx, f.org.ID, result.UserID, result.TenantID, &types.UpdateOrganizationRequest{})
		require.Error(t, denied)
	}
	sharedID, privateID := uuid.NewString(), uuid.NewString()
	require.NoError(t, f.db.Create(&types.KnowledgeBase{ID: sharedID, TenantID: f.org.OwnerTenantID, Name: "team-data"}).Error)
	require.NoError(t, f.db.Create(&types.KnowledgeBase{ID: privateID, TenantID: alice.TenantID, Name: "alice-private"}).Error)
	require.NoError(t, f.db.Create(&types.KnowledgeBaseShare{ID: uuid.NewString(), KnowledgeBaseID: sharedID, OrganizationID: f.org.ID, SourceTenantID: f.org.OwnerTenantID, SharedByUserID: f.org.OwnerID, Permission: types.OrgRoleEditor}).Error)
	role, visible, err := f.shares.CheckTenantKBPermission(ctx, sharedID, bob.TenantID, types.TenantRoleOwner)
	require.NoError(t, err)
	require.True(t, visible)
	require.Equal(t, types.OrgRoleEditor, role)
	_, visible, err = f.shares.CheckTenantKBPermission(ctx, privateID, bob.TenantID, types.TenantRoleOwner)
	require.NoError(t, err)
	require.False(t, visible)
}

func TestRetriesConcurrencyAndRestartKeepOneIdentityAndKey(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	results := make(chan *Result, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := f.s.apply(ctx, f.source, request("alice", 1), "ensure")
			if err != nil {
				failures <- err
			} else {
				results <- value
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var first *Result
	for value := range results {
		if first == nil {
			first = value
		}
		require.Equal(t, *first, *value)
	}
	require.NotNil(t, first)
	var count int64
	require.NoError(t, f.db.Model(&types.User{}).Where("id = ?", first.UserID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, f.db.Model(&types.TenantAPIKey{}).Where("tenant_id = ? AND revoked_at IS NULL", first.TenantID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	// Simulate loss of the final user binding after the native registration
	// committed; only the persisted random password proves recovery ownership.
	require.NoError(t, f.db.Model(&Binding{}).Where("subject = ?", "alice").Updates(map[string]any{"user_id": "", "tenant_id": 0, "api_key_id": 0}).Error)
	restarted, err := newService(f.db, f.s.users, f.s.orgs, f.s.keys, &Config{Sources: []SourceConfig{f.source}})
	require.NoError(t, err)
	again, err := restarted.apply(ctx, f.source, request("alice", 1), "ensure")
	require.NoError(t, err)
	require.Equal(t, *first, *again)
	var binding Binding
	require.NoError(t, f.db.First(&binding).Error)
	require.Contains(t, binding.BootstrapSecret, "enc:v1:")
}

func TestJoinFailureResumesAndUsernameCollisionNeverAdoptsAnExistingUser(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	old, err := f.s.users.Register(ctx, &types.RegisterRequest{Username: "alice", Email: "old@example.test", Password: "old-password"})
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.Organization{}).Where("id = ?", f.org.ID).Update("member_limit", 1).Error)
	_, err = f.s.apply(ctx, f.source, request("alice", 1), "ensure")
	require.Error(t, err)
	var binding Binding
	require.NoError(t, f.db.Where("subject = ?", "alice").First(&binding).Error)
	require.NotEmpty(t, binding.UserID)
	require.NotEqual(t, old.ID, binding.UserID)
	require.NotEqual(t, "alice", binding.Username)
	require.NoError(t, f.db.Model(&types.Organization{}).Where("id = ?", f.org.ID).Update("member_limit", 0).Error)
	result, err := f.s.apply(ctx, f.source, request("alice", 1), "ensure")
	require.NoError(t, err)
	require.Equal(t, binding.UserID, result.UserID)
	oldReloaded, err := f.s.users.GetUserByID(ctx, old.ID)
	require.NoError(t, err)
	require.Equal(t, old.TenantID, oldReloaded.TenantID)
}

func TestRevocationFencesStaleEnsureAndPreservesData(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first, err := f.s.apply(ctx, f.source, request("alice", 1), "ensure")
	require.NoError(t, err)
	_, err = f.s.apply(ctx, f.source, request("alice", 2), "revoke")
	require.NoError(t, err)
	_, err = f.s.keys.AuthenticateAPIKey(ctx, first.APIKey)
	require.Error(t, err)
	_, err = f.s.apply(ctx, f.source, request("alice", 1), "ensure")
	require.Error(t, err)
	_, err = f.s.apply(ctx, f.source, request("alice", 2), "ensure")
	require.Error(t, err)
	again, err := f.s.apply(ctx, f.source, request("alice", 3), "ensure")
	require.NoError(t, err)
	require.Equal(t, first.UserID, again.UserID)
	require.Equal(t, first.TenantID, again.TenantID)
	require.NotEqual(t, first.APIKey, again.APIKey)
	member, err := f.s.orgs.GetTenantMember(ctx, f.org.ID, first.TenantID)
	require.NoError(t, err)
	require.Equal(t, types.OrgRoleEditor, member.Role)
}

func TestDedicatedHTTPAuthenticationAndDisabledExtension(t *testing.T) {
	f := setup(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	f.s.Register(r)
	body, _ := json.Marshal(request("alice", 1))
	for _, headers := range []map[string]string{{}, {"Authorization": "Bearer wrong"}, {"Authorization": "Bearer fixture-provisioner-credential-123456789", "Origin": "https://browser.test"}, {"Authorization": "Bearer fixture-provisioner-credential-123456789", "Cookie": "browser=1"}} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/external-accounts/dsh-hub/ensure", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/external-accounts/dsh-hub/ensure", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer fixture-provisioner-credential-123456789")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var response struct {
		Success bool   `json:"success"`
		Data    Result `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.NotEmpty(t, response.Data.APIKey)
	disabled, err := newService(f.db, f.s.users, f.s.orgs, f.s.keys, nil)
	require.NoError(t, err)
	off := gin.New()
	disabled.Register(off)
	w = httptest.NewRecorder()
	off.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/integrations/external-accounts/dsh-hub/ensure", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestPrivateConfigurationKeepsRoleAndSourceServerControlled(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "bridge.key")
	cfgFile := filepath.Join(dir, "bridge.json")
	require.NoError(t, os.WriteFile(tokenFile, []byte("fixture-provisioner-credential-123456789"), 0600))
	write := func(role string) {
		data, _ := json.Marshal(Config{Sources: []SourceConfig{{ID: "dsh-hub", Issuer: "fixture-platform", TokenFile: tokenFile, InviteCode: "fixture-invite-code", Role: types.OrgMemberRole(role)}}})
		require.NoError(t, os.WriteFile(cfgFile, data, 0600))
	}
	write("editor")
	cfg, err := loadConfig(cfgFile)
	require.NoError(t, err)
	require.Len(t, cfg.Sources, 1)
	write("admin")
	_, err = loadConfig(cfgFile)
	require.Error(t, err)
	write("editor")
	require.NoError(t, os.Chmod(tokenFile, 0644))
	_, err = loadConfig(cfgFile)
	require.Error(t, err)
}

// Use only a disposable database named weknora_identity_test. This separately
// exercises the PostgreSQL row lock across independently constructed services.
func TestPostgresSerializesIndependentServices(t *testing.T) {
	dsn := os.Getenv("WEKNORA_IDENTITY_TEST_POSTGRES")
	if dsn == "" {
		t.Skip("optional disposable PostgreSQL fixture")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var name string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	require.Equal(t, "weknora_identity_test", name, "refusing a non-fixture database")
	schema := "identity_" + fmt.Sprintf("%x", uuid.New())
	require.NoError(t, db.Exec("CREATE SCHEMA "+schema).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Exec("DROP SCHEMA "+schema+" CASCADE").Error); _ = sqlDB.Close() })
	connectionConfig, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	connectionConfig.RuntimeParams["search_path"] = schema
	connection := stdlib.OpenDB(*connectionConfig)
	scoped, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	f := setupDatabase(t, scoped)
	other, err := newService(scoped, f.s.users, f.s.orgs, f.s.keys, &Config{Sources: []SourceConfig{f.source}})
	require.NoError(t, err)
	ctx := context.Background()
	results := make(chan *Result, 12)
	failures := make(chan error, 12)
	var wg sync.WaitGroup
	for i := range 12 {
		current := f.s
		if i%2 == 0 {
			current = other
		}
		wg.Go(func() {
			value, err := current.apply(ctx, f.source, request("alice", 1), "ensure")
			if err != nil {
				failures <- err
			} else {
				results <- value
			}
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var first *Result
	for value := range results {
		if first == nil {
			first = value
		}
		require.Equal(t, *first, *value)
	}
	require.NotNil(t, first)
	var count int64
	require.NoError(t, scoped.Model(&Binding{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, scoped.Model(&types.TenantAPIKey{}).Where("tenant_id = ? AND revoked_at IS NULL", first.TenantID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	_, err = other.apply(ctx, f.source, request("alice", 2), "revoke")
	require.NoError(t, err)
	_, err = f.s.apply(ctx, f.source, request("alice", 1), "ensure")
	require.Error(t, err)
	_, err = f.s.keys.AuthenticateAPIKey(ctx, first.APIKey)
	require.Error(t, err)
}

// TestHubFixture serves only an isolated, explicitly requested interoperability
// fixture. No production configuration or database is read by this test.
func TestHubFixture(t *testing.T) {
	dir := os.Getenv("WEKNORA_BRIDGE_FIXTURE_DIR")
	if dir == "" {
		t.Skip("used by DSH accept:weknora only")
	}
	f := setup(t)
	f.source.Issuer = os.Getenv("WEKNORA_BRIDGE_FIXTURE_ISSUER")
	require.True(t, identifier.MatchString(f.source.Issuer))
	f.s.sources[f.source.ID] = f.source
	gin.SetMode(gin.TestMode)
	r := gin.New()
	f.s.Register(r)
	r.GET("/api/v1/knowledge-bases", func(c *gin.Context) {
		key, err := f.s.keys.AuthenticateAPIKey(c.Request.Context(), c.GetHeader("X-API-Key"))
		if err != nil {
			c.JSON(401, gin.H{"error": "invalid key"})
			return
		}
		if c.GetHeader("X-Tenant-ID") != fmt.Sprint(key.TenantIDValue()) {
			c.JSON(403, gin.H{"error": "wrong tenant"})
			return
		}
		c.JSON(200, gin.H{"success": true, "data": []any{}})
	})
	server := httptest.NewServer(r)
	defer server.Close()
	data, _ := json.Marshal(map[string]any{"url": server.URL, "organizationId": f.org.ID})
	temporary := filepath.Join(dir, "ready.tmp")
	require.NoError(t, os.WriteFile(temporary, data, 0600))
	require.NoError(t, os.Rename(temporary, filepath.Join(dir, "ready.json")))
	deadline := time.Now().Add(100 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("bridge fixture was not stopped by its owner")
}
