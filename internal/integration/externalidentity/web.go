package externalidentity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const WebPrefix = "/external-app/"

// Only digests and native token IDs are persisted, never browser credentials.
type WebSession struct {
	Digest        string `gorm:"primaryKey;size:64"`
	Source        string `gorm:"size:128;index:external_web_binding"`
	Subject       string `gorm:"size:128;index:external_web_binding"`
	Revision      uint64
	ParentOrigin  string
	ExpiresAt     time.Time `gorm:"index"`
	ConsumedAt    *time.Time
	AccessTokenID string `gorm:"size:36"`
	TokenExpires  *time.Time
}

func (WebSession) TableName() string { return "integration_external_web_sessions" }

type WebRequest struct {
	Request
	ParentOrigin string `json:"parent_origin"`
}

type WebLaunch struct {
	URL          string `json:"url"`
	Ticket       string `json:"ticket"`
	ExpiresIn    int    `json:"expires_in"`
	ParentOrigin string `json:"parent_origin"`
}

type webAccessTokenIssuer interface {
	GeneratePersonalAccessToken(context.Context, *types.User, time.Duration) (string, error)
}

func ticketDigest(ticket string) string {
	hash := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(hash[:])
}

func trustedParent(source SourceConfig, parent string) bool {
	if source.Web == nil {
		return false
	}
	for _, allowed := range source.Web.ParentOrigins {
		if parent == allowed {
			return true
		}
	}
	return false
}

// withWebBinding uses the same locking order as ensure/revoke. SQLite service
// calls must remain in autocommit mode, as in apply.
func (s *Service) withWebBinding(ctx context.Context, source SourceConfig, subject string, fn func(*gorm.DB, *Binding) error) error {
	hash := identityHash(source, subject)
	lock := &s.locks[int(hash[0])%len(s.locks)]
	lock.Lock()
	defer lock.Unlock()
	process := func(tx *gorm.DB) error {
		var binding Binding
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("source = ? AND subject = ?", source.ID, subject).First(&binding).Error; err != nil {
			return fail("WEB_SESSION_UNAVAILABLE", 401)
		}
		if binding.Issuer != source.Issuer || binding.Desired != "ensure" || binding.State != "ready" || binding.APIKeyID == 0 {
			return fail("WEB_SESSION_UNAVAILABLE", 401)
		}
		return fn(tx, &binding)
	}
	if s.db.Dialector.Name() == "sqlite" {
		return process(s.db.WithContext(ctx))
	}
	return s.db.WithContext(ctx).Transaction(process)
}

func (s *Service) launchWeb(ctx context.Context, source SourceConfig, req WebRequest) (*WebLaunch, error) {
	if !trustedParent(source, req.ParentOrigin) || req.Issuer != source.Issuer || !identifier.MatchString(req.Subject) {
		return nil, fail("WEB_INTEGRATION_FORBIDDEN", 403)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	ticket := base64.RawURLEncoding.EncodeToString(raw)
	err := s.withWebBinding(ctx, source, req.Subject, func(tx *gorm.DB, binding *Binding) error {
		if req.Revision != binding.Revision {
			return fail("IDENTITY_REVISION_CONFLICT", 409)
		}
		// Bound abandoned tickets per identity. A live iframe's already consumed
		// session is retained until its access token expires or is revoked.
		if err := tx.Where("source = ? AND subject = ? AND ((access_token_id = '' AND expires_at < ?) OR token_expires < ?)", source.ID, req.Subject, time.Now(), time.Now()).Delete(&WebSession{}).Error; err != nil {
			return err
		}
		var outstanding int64
		if err := tx.Model(&WebSession{}).Where("source = ? AND subject = ? AND consumed_at IS NULL", source.ID, req.Subject).Count(&outstanding).Error; err != nil {
			return err
		}
		if outstanding >= 32 {
			return fail("WEB_SESSION_RATE_LIMITED", 429)
		}
		return tx.Create(&WebSession{Digest: ticketDigest(ticket), Source: source.ID, Subject: req.Subject, Revision: req.Revision, ParentOrigin: req.ParentOrigin, ExpiresAt: time.Now().Add(time.Minute)}).Error
	})
	if err != nil {
		return nil, err
	}
	return &WebLaunch{URL: source.Web.Origin + WebPrefix + source.ID + "/platform/knowledge-bases", Ticket: ticket, ExpiresIn: 60, ParentOrigin: req.ParentOrigin}, nil
}

func (s *Service) exchangeWeb(ctx context.Context, source SourceConfig, ticket, parent string) (string, error) {
	if len(ticket) != 43 || !trustedParent(source, parent) {
		return "", fail("WEB_TICKET_INVALID", 401)
	}
	var issued WebSession
	if err := s.db.WithContext(ctx).Where("digest = ? AND source = ?", ticketDigest(ticket), source.ID).First(&issued).Error; err != nil {
		return "", fail("WEB_TICKET_INVALID", 401)
	}
	var access string
	err := s.withWebBinding(ctx, source, issued.Subject, func(tx *gorm.DB, binding *Binding) error {
		if issued.Revision != binding.Revision || issued.ParentOrigin != parent {
			return fail("WEB_TICKET_INVALID", 401)
		}
		now := time.Now()
		used := tx.Model(&WebSession{}).Where("digest = ? AND consumed_at IS NULL AND expires_at > ?", issued.Digest, now).Update("consumed_at", now)
		if used.Error != nil {
			return used.Error
		}
		if used.RowsAffected != 1 {
			return fail("WEB_TICKET_INVALID", 401)
		}
		user, err := s.users.GetUserByID(ctx, binding.UserID)
		if err != nil || user == nil || !user.IsActive {
			return fail("WEB_ACCOUNT_UNAVAILABLE", 401)
		}
		// Native access credentials retain all existing RBAC and business APIs.
		// Renewal must return through the currently authenticated DSH parent.
		issuer, ok := s.users.(webAccessTokenIssuer)
		if !ok || user.TenantID != binding.TenantID {
			return errors.New("personal web token issuer unavailable")
		}
		access, err = issuer.GeneratePersonalAccessToken(ctx, user, 15*time.Minute)
		if err != nil {
			return err
		}
		cleanup := func() { _ = s.users.RevokeToken(ctx, access) }
		record, err := s.users.GetAccessTokenByValue(ctx, access)
		if err != nil || record == nil {
			cleanup()
			return errors.New("web access token was not persisted")
		}
		if err = tx.Model(&WebSession{}).Where("digest = ?", issued.Digest).Updates(map[string]any{"access_token_id": record.ID, "token_expires": record.ExpiresAt}).Error; err != nil {
			cleanup()
			return err
		}
		return nil
	})
	if err != nil && access != "" {
		_ = s.users.RevokeToken(ctx, access)
	}
	return access, err
}

func (s *Service) revokeWebSessions(ctx context.Context, binding *Binding) error {
	query := s.db.WithContext(ctx).Model(&WebSession{}).Select("access_token_id").Where("source = ? AND subject = ? AND access_token_id <> ''", binding.Source, binding.Subject)
	if err := s.db.WithContext(ctx).Model(&types.AuthToken{}).Where("id IN (?)", query).Update("is_revoked", true).Error; err != nil {
		return err
	}
	return s.db.WithContext(ctx).Where("source = ? AND subject = ?", binding.Source, binding.Subject).Delete(&WebSession{}).Error
}

// FramePolicy is shared by nginx's auth_request and the bundled Go frontend.
func (s *Service) FramePolicy(path string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(path, WebPrefix), "/")
	if !strings.HasPrefix(path, WebPrefix) || len(parts) < 2 || !identifier.MatchString(parts[0]) {
		return "frame-ancestors 'none'", false
	}
	source, ok := s.sources[parts[0]]
	if !ok || source.Web == nil {
		return "frame-ancestors 'none'", false
	}
	return "frame-ancestors " + strings.Join(source.Web.ParentOrigins, " "), true
}

func (s *Service) registerWeb(r gin.IRoutes) {
	r.GET("/api/v1/integrations/external-app/:source/config", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		source, ok := s.sources[c.Param("source")]
		if !ok || source.Web == nil {
			c.AbortWithStatus(404)
			return
		}
		c.JSON(200, gin.H{"parent_origins": source.Web.ParentOrigins})
	})
	r.GET("/api/v1/integrations/external-app/frame-policy", func(c *gin.Context) {
		u, err := url.ParseRequestURI(c.GetHeader("X-External-App-URI"))
		if err != nil {
			c.AbortWithStatus(403)
			return
		}
		policy, ok := s.FramePolicy(u.Path)
		c.Header("Content-Security-Policy", policy)
		c.Header("Cache-Control", "no-store")
		if !ok {
			c.AbortWithStatus(403)
			return
		}
		c.Status(200)
	})
	r.POST("/api/v1/integrations/external-app/:source/exchange", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		source, exists := s.sources[c.Param("source")]
		if !exists || source.Web == nil || c.GetHeader("Origin") != source.Web.Origin || c.GetHeader("Authorization") != "" {
			c.AbortWithStatus(403)
			return
		}
		var req struct {
			Ticket       string `json:"ticket"`
			ParentOrigin string `json:"parent_origin"`
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
		if c.ShouldBindJSON(&req) != nil {
			c.AbortWithStatus(400)
			return
		}
		access, err := s.exchangeWeb(c.Request.Context(), source, req.Ticket, req.ParentOrigin)
		if err != nil {
			webFailure(c, err)
			return
		}
		record, err := s.users.GetAccessTokenByValue(c.Request.Context(), access)
		if err != nil || record == nil {
			_ = s.users.RevokeToken(c.Request.Context(), access)
			webFailure(c, errors.New("web access token missing"))
			return
		}
		c.JSON(200, gin.H{"success": true, "data": gin.H{"token": access, "user_id": record.UserID}})
	})
}

func webFailure(c *gin.Context, err error) {
	var known *bridgeError
	if errors.As(err, &known) {
		c.JSON(known.status, gin.H{"error": known.code})
		return
	}
	c.JSON(503, gin.H{"error": "WEB_SESSION_UNAVAILABLE"})
}
