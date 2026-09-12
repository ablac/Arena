package config

import "testing"

func TestCustomerOIDCCallbackAllowlist(t *testing.T) {
	c := Config{CustomerOIDCRedirectURI: "https://arena.angel-serv.com/account/callback", CustomerOIDCAdditionalRedirectURIs: "https://arena.angel-gaming.com/account/callback"}
	if urls, err := CustomerOIDCRedirectURIs(c); err != nil || len(urls) != 2 {
		t.Fatalf("valid config: %v %v", urls, err)
	}
	for _, bad := range []string{"http://arena.example/account/callback", "https://user@arena.example/account/callback", "https://arena.example/account/callback?evil=1", "https://arena.example/account/callback#x", "https://arena.example/other", "https://arena.example/%61ccount/callback", ",", "https://arena.example/account/callback?"} {
		c.CustomerOIDCAdditionalRedirectURIs = bad
		if _, err := CustomerOIDCRedirectURIs(c); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
