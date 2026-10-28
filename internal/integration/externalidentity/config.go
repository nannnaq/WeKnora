// Package externalidentity provides an optional, server-to-server account bridge.
// It owns its schema and authentication; public login/registration are unchanged.
package externalidentity

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const ConfigEnvironment = "WEKNORA_EXTERNAL_IDENTITY_CONFIG_FILE"

type SourceConfig struct {
	ID         string              `json:"id"`
	Issuer     string              `json:"issuer"`
	TokenFile  string              `json:"token_file"`
	InviteCode string              `json:"invite_code"`
	Role       types.OrgMemberRole `json:"role"`
	tokenHash  [32]byte
}

type Config struct {
	Sources []SourceConfig `json:"sources"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)

func privateFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("external identity configuration and token files must be private regular files")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("external identity file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("external identity file exceeds its size limit")
	}
	return data, nil
}

func loadConfig(path string) (*Config, error) {
	if path == "" {
		return nil, nil
	}
	data, err := privateFile(path, 64*1024)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, errors.New("invalid external identity configuration JSON")
	}
	if len(cfg.Sources) == 0 || len(cfg.Sources) > 16 {
		return nil, errors.New("external identity requires 1–16 trusted sources")
	}
	seen := make(map[string]bool)
	for i := range cfg.Sources {
		source := &cfg.Sources[i]
		if !identifier.MatchString(source.ID) || !identifier.MatchString(source.Issuer) || seen[source.ID] || len(source.InviteCode) < 8 || len(source.InviteCode) > 32 {
			return nil, errors.New("invalid external identity source, issuer or invitation")
		}
		if source.Role == "" {
			source.Role = types.OrgRoleViewer
		}
		if source.Role != types.OrgRoleViewer && source.Role != types.OrgRoleEditor {
			return nil, errors.New("external identity sources may grant viewer or editor only")
		}
		token, err := privateFile(source.TokenFile, 8192)
		if err != nil {
			return nil, err
		}
		secret := strings.TrimSpace(string(token))
		if len(secret) < 32 || strings.ContainsAny(secret, "\r\n") {
			return nil, errors.New("external identity tokens require at least 32 characters")
		}
		source.tokenHash = sha256.Sum256([]byte(secret))
		seen[source.ID] = true
	}
	return &cfg, nil
}
