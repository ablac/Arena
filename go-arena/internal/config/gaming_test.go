package config

import (
	"strings"
	"testing"
)

func TestValidateGamingConfig(t *testing.T) {
	if err := ValidateGamingConfig(Config{}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGamingConfig(Config{GamingOrigin: "https://angel-gaming.com", GamingServiceKey: strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{
		{GamingOrigin: "http://angel-gaming.com", GamingServiceKey: strings.Repeat("a", 64)},
		{GamingOrigin: "https://user:pass@angel-gaming.com", GamingServiceKey: strings.Repeat("a", 64)},
		{GamingOrigin: "https://angel-gaming.com/path", GamingServiceKey: strings.Repeat("a", 64)},
		{GamingOrigin: "https://angel-gaming.com", GamingServiceKey: "short"},
		{GamingServiceKey: strings.Repeat("a", 64)},
	} {
		if err := ValidateGamingConfig(cfg); err == nil {
			t.Fatal("invalid Gaming configuration accepted")
		}
	}
}
