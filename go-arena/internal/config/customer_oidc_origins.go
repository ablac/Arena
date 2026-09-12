package config

import (
	"fmt"
	"net/url"
	"strings"
)

// CustomerOIDCRedirectURIs enables origin selection only with an explicit
// additional callback allowlist. The primary remains the legacy-row fallback.
func CustomerOIDCRedirectURIs(c Config) ([]string, error) {
	if strings.TrimSpace(c.CustomerOIDCAdditionalRedirectURIs) == "" {
		return nil, nil
	}
	raw := append([]string{c.CustomerOIDCRedirectURI}, strings.Split(c.CustomerOIDCAdditionalRedirectURIs, ",")...)
	if len(raw) > 8 {
		return nil, fmt.Errorf("too many customer OIDC callbacks")
	}
	result := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, value := range raw {
		value = strings.TrimSpace(value)
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || (u.Path != "/account/callback" && u.Path != "/arena/account/callback") || len(value) > 512 {
			return nil, fmt.Errorf("invalid customer OIDC callback allowlist")
		}
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result, nil
}
