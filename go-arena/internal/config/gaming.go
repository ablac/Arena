package config

import (
	"encoding/hex"
	"fmt"
	"net/url"
)

func ValidateGamingConfig(cfg Config) error {
	if cfg.GamingOrigin == "" && cfg.GamingServiceKey == "" {
		return nil
	}
	origin, err := url.Parse(cfg.GamingOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return fmt.Errorf("ANGEL_GAMING_ORIGIN must be an HTTPS origin without path, credentials or query")
	}
	key, err := hex.DecodeString(cfg.GamingServiceKey)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("ANGEL_GAMING_SERVICE_KEY must be a 64-character hexadecimal secret")
	}
	return nil
}
