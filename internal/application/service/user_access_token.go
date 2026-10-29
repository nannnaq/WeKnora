package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// GeneratePersonalAccessToken is an additive extension point for trusted
// integrations. It uses native validation and persistence but does not create a
// refresh token or alter ordinary login. Every issuance is independently
// revocable, including two issuances for the same user in the same second.
func (s *userService) GeneratePersonalAccessToken(ctx context.Context, user *types.User, ttl time.Duration) (string, error) {
	if user == nil || !user.IsActive || user.TenantID == 0 || ttl <= 0 || ttl > time.Hour {
		return "", errors.New("invalid personal access session")
	}
	now := time.Now()
	id := uuid.NewString()
	claims := jwt.MapClaims{"jti": id, "user_id": user.ID, "email": user.Email, "tenant_id": user.TenantID, "exp": now.Add(ttl).Unix(), "iat": now.Unix(), "type": "access"}
	value, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(getJwtSecret()))
	if err != nil {
		return "", err
	}
	record := &types.AuthToken{ID: id, UserID: user.ID, Token: value, TokenType: "access_token", ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now}
	if err := s.tokenRepo.CreateToken(ctx, record); err != nil {
		return "", fmt.Errorf("persist personal access token: %w", err)
	}
	return value, nil
}
