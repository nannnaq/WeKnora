package externalidentity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Binding belongs exclusively to this optional integration. Its extension
// tables are separate from upstream's numbered migrations.
type Binding struct {
	Source          string `gorm:"primaryKey;size:128"`
	Subject         string `gorm:"primaryKey;size:128"`
	Issuer          string `gorm:"size:128;not null"`
	Email           string `gorm:"size:255;not null"`
	Username        string `gorm:"size:50"`
	BootstrapSecret string `json:"-" gorm:"type:text;not null"`
	UserID          string `gorm:"size:36"`
	TenantID        uint64
	OrganizationID  string `gorm:"size:36"`
	APIKeyID        uint64
	Revision        uint64
	Desired         string `gorm:"size:16"`
	State           string `gorm:"size:16"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (Binding) TableName() string { return "integration_external_identity_bindings" }

type Request struct {
	Issuer   string `json:"issuer"`
	Subject  string `json:"subject"`
	Username string `json:"username"`
	Revision uint64 `json:"revision"`
}

type Result struct {
	UserID         string              `json:"user_id"`
	Username       string              `json:"username"`
	TenantID       uint64              `json:"tenant_id"`
	APIKeyID       uint64              `json:"api_key_id"`
	APIKey         string              `json:"api_key"`
	OrganizationID string              `json:"organization_id"`
	Role           types.OrgMemberRole `json:"role"`
}

type bridgeError struct {
	code   string
	status int
}

func (e *bridgeError) Error() string     { return e.code }
func fail(code string, status int) error { return &bridgeError{code, status} }

type Service struct {
	db      *gorm.DB
	users   interfaces.UserService
	orgs    interfaces.OrganizationService
	keys    interfaces.TenantAPIKeyService
	sources map[string]SourceConfig
	// Striped locks bound memory, serialize SQLite and avoid concurrent inserts
	// before the cross-process binding-row lock can be acquired.
	locks [64]sync.Mutex
}

func New(db *gorm.DB, users interfaces.UserService, orgs interfaces.OrganizationService, keys interfaces.TenantAPIKeyService) (*Service, error) {
	cfg, err := loadConfig(os.Getenv(ConfigEnvironment))
	if err != nil {
		return nil, err
	}
	return newService(db, users, orgs, keys, cfg)
}

func newService(db *gorm.DB, users interfaces.UserService, orgs interfaces.OrganizationService, keys interfaces.TenantAPIKeyService, cfg *Config) (*Service, error) {
	s := &Service{db: db, users: users, orgs: orgs, keys: keys, sources: map[string]SourceConfig{}}
	if cfg == nil {
		return s, nil
	}
	if utils.GetAESKey() == nil {
		return nil, errors.New("external identity requires SYSTEM_AES_KEY for encrypted credentials")
	}
	if err := db.AutoMigrate(&Binding{}, &WebSession{}); err != nil {
		return nil, err
	}
	for _, source := range cfg.Sources {
		s.sources[source.ID] = source
	}
	return s, nil
}

func identityHash(source SourceConfig, subject string) string {
	h := sha256.Sum256([]byte(source.ID + "\x00" + source.Issuer + "\x00" + subject))
	return hex.EncodeToString(h[:])
}

// apply persists intent even when a downstream step fails. A later retry can
// recover created resources without changing identity or reviving a stale key.
func (s *Service) apply(ctx context.Context, source SourceConfig, req Request, action string) (*Result, error) {
	if req.Issuer != source.Issuer || !identifier.MatchString(req.Subject) || req.Revision == 0 || req.Revision > 9007199254740991 {
		return nil, fail("IDENTITY_INVALID", 400)
	}
	if action == "ensure" && (len(req.Username) < 2 || len(req.Username) > 50 || !identifier.MatchString(req.Username)) {
		return nil, fail("IDENTITY_INVALID", 400)
	}
	hash := identityHash(source, req.Subject)
	lock := &s.locks[int(hash[0])%len(s.locks)]
	lock.Lock()
	defer lock.Unlock()
	random := make([]byte, 21)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	secret, err := utils.EncryptAESGCM("Aa1!"+base64.RawURLEncoding.EncodeToString(random), utils.GetAESKey())
	if err != nil {
		return nil, err
	}
	binding := Binding{Source: source.ID, Subject: req.Subject, Issuer: source.Issuer, Email: "dsh-" + hash[:40] + "@accounts.dsh.invalid", BootstrapSecret: secret}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&binding).Error; err != nil {
		return nil, err
	}
	var result *Result
	var operationErr error
	process := func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source = ? AND subject = ?", source.ID, req.Subject).First(&binding).Error; err != nil {
			return err
		}
		if binding.Issuer != source.Issuer {
			return fail("IDENTITY_SOURCE_CHANGED", 409)
		}
		if req.Revision < binding.Revision || req.Revision == binding.Revision && binding.Desired != action {
			return fail("IDENTITY_REVISION_CONFLICT", 409)
		}
		binding.Revision, binding.Desired, binding.State = req.Revision, action, "pending"
		if action == "revoke" {
			operationErr = s.revoke(ctx, &binding)
		} else {
			result, operationErr = s.ensure(ctx, source, req, &binding)
		}
		if operationErr == nil {
			binding.State = "ready"
		}
		return tx.Save(&binding).Error
	}
	// SQLite has a database-wide write lock. Service calls own their normal
	// transactions; keep the durable bridge steps in autocommit mode there.
	// PostgreSQL holds the binding row lock across those independently committed
	// service calls, allowing separate bridge processes to recover safely.
	if s.db.Dialector.Name() == "sqlite" {
		err = process(s.db.WithContext(ctx))
	} else {
		err = s.db.WithContext(ctx).Transaction(process)
	}
	if err != nil {
		return nil, err
	}
	return result, operationErr
}

func (s *Service) ensure(ctx context.Context, source SourceConfig, req Request, binding *Binding) (*Result, error) {
	password, err := utils.DecryptStoredSecret(binding.BootstrapSecret)
	if err != nil {
		return nil, err
	}
	var user *types.User
	if binding.UserID != "" {
		user, err = s.users.GetUserByID(ctx, binding.UserID)
		if err != nil || user == nil {
			return nil, fail("IDENTITY_ACCOUNT_MISSING", 409)
		}
	} else {
		// A lost registration response is recoverable only with proof of our
		// persisted random bootstrap secret, never by username/email alone.
		user, _ = s.users.GetUserByEmail(ctx, binding.Email)
		if user != nil && s.users.ValidatePassword(ctx, user.ID, password) != nil {
			return nil, fail("IDENTITY_ACCOUNT_CONFLICT", 409)
		}
		if user == nil {
			username := req.Username
			if existing, _ := s.users.GetUserByUsername(ctx, username); existing != nil {
				username = req.Username[:min(len(req.Username), 37)] + "-" + hashSuffix(source, req.Subject)
			}
			user, err = s.users.Register(ctx, &types.RegisterRequest{Username: username, Email: binding.Email, Password: password, TenantProvisioning: types.TenantProvisioningCreatePersonal})
			if err != nil {
				return nil, fail("IDENTITY_REGISTRATION_FAILED", 503)
			}
		}
		binding.UserID, binding.TenantID, binding.Username = user.ID, user.TenantID, user.Username
	}
	if !user.IsActive || user.IsSystemAdmin || user.CanAccessAllTenants || user.TenantID == 0 || user.TenantID != binding.TenantID {
		return nil, fail("IDENTITY_ACCOUNT_UNAVAILABLE", 409)
	}
	org, err := s.orgs.GetOrganizationByInviteCode(ctx, source.InviteCode)
	if err != nil || org == nil || org.RequireApproval {
		return nil, fail("IDENTITY_INVITATION_UNAVAILABLE", 409)
	}
	if binding.OrganizationID != "" && binding.OrganizationID != org.ID {
		return nil, fail("IDENTITY_ORGANIZATION_CHANGED", 409)
	}
	binding.OrganizationID = org.ID
	member, memberErr := s.orgs.GetTenantMember(ctx, org.ID, user.TenantID)
	if memberErr != nil || member == nil {
		if err := s.orgs.AddTenantMember(ctx, org.ID, user.TenantID, user.ID, source.Role); err != nil {
			return nil, fail("IDENTITY_JOIN_FAILED", 503)
		}
	} else if member.Role != source.Role {
		// Configured sources are explicitly delegated this bounded role grant.
		if err := s.orgs.UpdateTenantMemberRole(ctx, org.ID, user.TenantID, source.Role, org.OwnerID, org.OwnerTenantID); err != nil {
			return nil, fail("IDENTITY_JOIN_FAILED", 503)
		}
	}
	name := "integration:" + identityHash(source, req.Subject)[:32] + ":" + fmt.Sprint(req.Revision)
	keys, err := s.keys.ListAPIKeys(ctx, binding.TenantID)
	if err != nil {
		return nil, err
	}
	var key *types.TenantAPIKey
	for _, candidate := range keys {
		if candidate.Name == name && candidate.RevokedAt == nil && (candidate.ExpiresAt == nil || candidate.ExpiresAt.After(time.Now())) {
			key = candidate
			break
		}
	}
	if key == nil {
		created, err := s.keys.CreateAPIKey(ctx, interfaces.TenantAPIKeyCreateRequest{TenantID: binding.TenantID, Name: name, FullAccess: true, ScopeType: types.APIKeyScopeTenant})
		if err != nil {
			return nil, err
		}
		key = created.APIKey
		key.APIKey = created.Token
	}
	if key.IsPlatform() || key.TenantIDValue() != binding.TenantID || !key.FullAccess || key.APIKey == "" {
		return nil, fail("IDENTITY_KEY_INVALID", 409)
	}
	binding.APIKeyID = key.ID
	// Retire older keys after ensuring the replacement; recovery also cleans up
	// keys from requests whose response or final binding write was interrupted.
	for _, candidate := range keys {
		if candidate.ID != key.ID && candidate.RevokedAt == nil && strings.HasPrefix(candidate.Name, "integration:"+identityHash(source, req.Subject)[:32]+":") {
			if err := s.keys.RevokeAPIKey(ctx, binding.TenantID, candidate.ID); err != nil {
				return nil, err
			}
		}
	}
	return &Result{UserID: user.ID, Username: user.Username, TenantID: binding.TenantID, APIKeyID: key.ID, APIKey: key.APIKey, OrganizationID: org.ID, Role: source.Role}, nil
}

func hashSuffix(source SourceConfig, subject string) string {
	return identityHash(source, subject)[:12]
}

func (s *Service) revoke(ctx context.Context, binding *Binding) error {
	if err := s.revokeWebSessions(ctx, binding); err != nil {
		return err
	}
	if binding.TenantID == 0 {
		return nil
	}
	source := s.sources[binding.Source]
	keys, err := s.keys.ListAPIKeys(ctx, binding.TenantID)
	if err != nil {
		return err
	}
	prefix := "integration:" + identityHash(source, binding.Subject)[:32] + ":"
	for _, key := range keys {
		if key.RevokedAt == nil && strings.HasPrefix(key.Name, prefix) {
			if err := s.keys.RevokeAPIKey(ctx, binding.TenantID, key.ID); err != nil {
				return err
			}
		}
	}
	binding.APIKeyID = 0
	return nil
}
