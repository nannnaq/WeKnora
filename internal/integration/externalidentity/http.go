package externalidentity

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const Route = "/api/v1/integrations/external-accounts/:source/:action"

// Register installs dedicated machine authentication before global JWT auth.
// No cookie, user password, API key or browser identity grants access here.
func (s *Service) Register(r gin.IRoutes) {
	if s == nil || len(s.sources) == 0 {
		return
	}
	s.registerWeb(r)
	r.POST(Route, func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		source, exists := s.sources[c.Param("source")]
		authorization := c.GetHeader("Authorization")
		provided := sha256.Sum256([]byte(strings.TrimPrefix(authorization, "Bearer ")))
		if !exists || !strings.HasPrefix(authorization, "Bearer ") || subtle.ConstantTimeCompare(provided[:], source.tokenHash[:]) != 1 || c.GetHeader("Origin") != "" || c.GetHeader("Cookie") != "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "EXTERNAL_IDENTITY_AUTH_REQUIRED"})
			return
		}
		action := c.Param("action")
		if action == "web-session" {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
			var req WebRequest
			if c.ShouldBindJSON(&req) != nil {
				c.AbortWithStatus(400)
				return
			}
			result, err := s.launchWeb(c.Request.Context(), source, req)
			if err != nil {
				webFailure(c, err)
				return
			}
			c.JSON(200, gin.H{"success": true, "data": result})
			return
		}
		if action != "ensure" && action != "revoke" {
			c.JSON(http.StatusNotFound, gin.H{"error": "NOT_FOUND"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		var req Request
		if c.ShouldBindJSON(&req) != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "IDENTITY_INVALID"})
			return
		}
		result, err := s.apply(c.Request.Context(), source, req, action)
		if err != nil {
			var known *bridgeError
			if errors.As(err, &known) {
				c.JSON(known.status, gin.H{"error": known.code})
				return
			}
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "EXTERNAL_IDENTITY_UNAVAILABLE"})
			return
		}
		if action == "revoke" {
			c.JSON(http.StatusOK, gin.H{"success": true})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
	})
}
