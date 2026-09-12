package api

import (
	"golang.org/x/oauth2"
	"net/http"
	"net/url"
	"strings"
)

// Match the actual Host and trusted transport, never X-Forwarded-Host. Every
// redirect remains a literal operator-configured URL, not request interpolation.
func customerCallbackOriginMatches(r *http.Request, callback string, path bool) bool {
	u, err := url.Parse(callback)
	if err != nil {
		return false
	}
	scheme := "http"
	if secureCookie(r) {
		scheme = "https"
	}
	return u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) && (!path || u.Path == r.URL.Path)
}
func (h *CustomerOIDCHandler) loginOAuthConfig(r *http.Request) (oauth2.Config, bool) {
	c := *h.oauth2Config
	if len(h.redirectURIs) == 0 {
		return c, true
	}
	for _, uri := range h.redirectURIs {
		if customerCallbackOriginMatches(r, uri, false) {
			c.RedirectURL = uri
			return c, true
		}
	}
	return c, false
}
func (h *CustomerOIDCHandler) callbackOAuthConfig(r *http.Request, txn customerOIDCTransaction) (oauth2.Config, bool) {
	c := *h.oauth2Config
	uri := txn.RedirectURI
	if uri == "" {
		uri = c.RedirectURL
	} // Only transactions predating this field.
	if len(h.redirectURIs) == 0 {
		if uri != c.RedirectURL {
			return c, false
		}
		return c, true
	}
	for _, allowed := range h.redirectURIs {
		if uri == allowed && customerCallbackOriginMatches(r, uri, true) {
			c.RedirectURL = uri
			return c, true
		}
	}
	return c, false
}
