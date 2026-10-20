package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/dujiao-next/internal/crypto"
	"github.com/dujiao-next/internal/modules/backup/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

// storedConfig is deliberately separate from Config: plaintext never reaches settings.
type storedConfig struct {
	domain.Config
	SecretCiphertext string `json:"secret_ciphertext,omitempty"`
}

func (s *Service) loadConfig() (storedConfig, error) {
	cfg := storedConfig{Config: domain.DefaultConfig()}
	if s.settings == nil {
		return cfg, domain.ErrNotReady
	}
	value, found, err := s.settings.GetByKey(domain.SettingKey)
	if err != nil {
		return cfg, err
	}
	if !found {
		return cfg, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return cfg, domain.ErrInvalidConfig
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, domain.ErrInvalidConfig
	}
	// Never accept a plaintext credential written through an unrelated settings endpoint.
	if cfg.SecretAccessKey != "" {
		return cfg, domain.ErrInvalidConfig
	}
	cfg.SecretConfigured = cfg.SecretCiphertext != ""
	return cfg, nil
}

func masked(cfg storedConfig) domain.Config {
	result := cfg.Config
	result.SecretAccessKey = ""
	result.SecretConfigured = cfg.SecretCiphertext != ""
	return result
}

func (s *Service) GetConfig() (domain.Config, error) {
	s.ops.Lock()
	defer s.ops.Unlock()
	cfg, err := s.loadConfig()
	if err != nil {
		return domain.Config{}, err
	}
	return masked(cfg), nil
}

func normalizeConfig(cfg domain.Config) domain.Config {
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	cfg.Prefix = strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
	cfg.DailyTime = strings.TrimSpace(cfg.DailyTime)
	cfg.Timezone = strings.TrimSpace(cfg.Timezone)
	cfg.SecretConfigured = false
	return cfg
}

func validateSchedule(cfg domain.Config) error {
	t, err := time.Parse("15:04", cfg.DailyTime)
	if err != nil || t.Format("15:04") != cfg.DailyTime {
		return domain.ErrInvalidConfig
	}
	if _, err := time.LoadLocation(cfg.Timezone); err != nil || cfg.Timezone == "" {
		return domain.ErrInvalidConfig
	}
	if cfg.RetainCount < 1 || cfg.RetainCount > 1000 {
		return domain.ErrInvalidConfig
	}
	return nil
}

func validateTarget(cfg domain.Config) error {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" {
		return domain.ErrInvalidConfig
	}
	if cfg.Bucket == "" || cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" || len(cfg.Prefix) > 512 {
		return domain.ErrInvalidConfig
	}
	if strings.ContainsAny(cfg.Bucket, "/\\\r\n\t ") || strings.ContainsAny(cfg.AccessKeyID, "\r\n") || strings.ContainsAny(cfg.Prefix, "\\\r\n\x00") {
		return domain.ErrInvalidConfig
	}
	for _, part := range strings.Split(cfg.Prefix, "/") {
		if part == "." || part == ".." {
			return domain.ErrInvalidConfig
		}
	}
	return nil
}

func (s *Service) decrypted(cfg storedConfig) (domain.Config, error) {
	if s.options.SecretKey == "" || cfg.SecretCiphertext == "" {
		return domain.Config{}, domain.ErrInvalidConfig
	}
	plain, err := crypto.Decrypt(crypto.DeriveKey(s.options.SecretKey), cfg.SecretCiphertext)
	if err != nil {
		return domain.Config{}, domain.ErrInvalidConfig
	}
	result := cfg.Config
	result.SecretAccessKey = plain
	if err := validateTarget(result); err != nil {
		return domain.Config{}, err
	}
	return result, nil
}

func (s *Service) objectConfig() (domain.Config, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		return domain.Config{}, err
	}
	return s.decrypted(cfg)
}

func (s *Service) UpdateConfig(input domain.Config) (domain.Config, error) {
	if !s.supported() {
		return domain.Config{}, domain.ErrUnsupported
	}
	s.ops.Lock()
	defer s.ops.Unlock()
	previous, err := s.loadConfig()
	if err != nil {
		return domain.Config{}, err
	}
	cfg := normalizeConfig(input)
	if err := validateSchedule(cfg); err != nil {
		return domain.Config{}, err
	}
	if cfg.AccessKeyID != previous.AccessKeyID && cfg.SecretAccessKey == "" && previous.SecretCiphertext != "" {
		return domain.Config{}, domain.ErrInvalidConfig
	}
	if cfg.Endpoint != previous.Endpoint || cfg.Bucket != previous.Bucket {
		if s.records == nil {
			return domain.Config{}, domain.ErrNotReady
		}
		ctx, cancel := s.shortContext(context.Background())
		_, count, err := s.records.List(ctx, 1, 0)
		cancel()
		if err != nil {
			return domain.Config{}, err
		}
		if count != 0 {
			return domain.Config{}, domain.ErrTargetChange
		}
	}
	stored := storedConfig{Config: cfg, SecretCiphertext: previous.SecretCiphertext}
	if cfg.SecretAccessKey != "" {
		if s.options.SecretKey == "" {
			return domain.Config{}, domain.ErrInvalidConfig
		}
		encrypted, err := crypto.Encrypt(crypto.DeriveKey(s.options.SecretKey), cfg.SecretAccessKey)
		if err != nil {
			return domain.Config{}, fmt.Errorf("encrypt backup credential: %w", domain.ErrInvalidConfig)
		}
		stored.SecretCiphertext = encrypted
	}
	stored.SecretAccessKey = ""
	if cfg.Enabled || cfg.Endpoint != "" || cfg.Bucket != "" || cfg.AccessKeyID != "" || stored.SecretCiphertext != "" {
		if _, err := s.decrypted(stored); err != nil {
			return domain.Config{}, err
		}
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return domain.Config{}, err
	}
	var value jsonmap.JSON
	if err := json.Unmarshal(raw, &value); err != nil {
		return domain.Config{}, err
	}
	if _, err := s.settings.Upsert(domain.SettingKey, value); err != nil {
		return domain.Config{}, err
	}
	select {
	case s.reload <- struct{}{}:
	default:
	}
	return masked(stored), nil
}
