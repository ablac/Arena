package api

// Demo-bot fleet management. The fleet runs as a separate container built
// from a PRIVATE repo (the public server carries no bot AI); arena-server
// only proxies between the admin panel and two internal control planes:
//
//   - the fleet's own control API (ARENA_DEMOBOTS_CONTROL_URL, shared
//     ARENA_ADMIN_TOKEN auth) for status / bot list / count scaling, and
//   - the arena-updater sidecar (ARENA_UPDATER_URL + shared secret) for
//     "update the fleet from the private repo" (see updater/demobots.mjs).
//
// Everything degrades to {"configured": false} when the fleet is not
// deployed, so the admin panel renders a clean disabled card on plain
// public-repo installs.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"arena-server/internal/config"
)

var fleetHTTPClient = &http.Client{Timeout: 10 * time.Second}

func fleetConfigured() bool {
	return config.C.DemobotsControlURL != "" && config.C.AdminToken != ""
}

func fleetControlURL(path string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(config.C.DemobotsControlURL))
	if err != nil {
		return "", err
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	return base.String(), nil
}

// proxyFleetControl forwards a request to the fleet's control API with the
// shared admin token and relays the JSON response (and status code) back.
func proxyFleetControl(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	if !fleetConfigured() {
		writeJSON(w, http.StatusOK, map[string]interface{}{"configured": false})
		return
	}
	target, err := fleetControlURL(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid ARENA_DEMOBOTS_CONTROL_URL")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build fleet request")
		return
	}
	req.Header.Set("X-Admin-Token", config.C.AdminToken)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := fleetHTTPClient.Do(req)
	if err != nil {
		// An unreachable fleet is a state, not a server error: the card shows it.
		writeJSON(w, http.StatusOK, map[string]interface{}{"configured": true, "reachable": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	var payload map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		writeError(w, http.StatusBadGateway, "fleet control returned invalid JSON")
		return
	}
	payload["configured"] = true
	payload["reachable"] = true
	writeJSON(w, resp.StatusCode, payload)
}

// GET /api/v1/admin/demobots/status
func (h *AdminHandler) demobotsStatus(w http.ResponseWriter, r *http.Request) {
	proxyFleetControl(w, r, http.MethodGet, "/control/status", nil)
}

// GET /api/v1/admin/demobots/config
func (h *AdminHandler) demobotsConfig(w http.ResponseWriter, r *http.Request) {
	proxyFleetControl(w, r, http.MethodGet, "/control/config", nil)
}

// PUT /api/v1/admin/demobots/count
func (h *AdminHandler) demobotsSetCount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count *int `json:"count"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Count == nil {
		writeError(w, http.StatusBadRequest, "body must be {\"count\": <number>}")
		return
	}
	if *req.Count < 0 || *req.Count > 100 {
		writeError(w, http.StatusBadRequest, "count must be between 0 and 100")
		return
	}
	body, _ := json.Marshal(map[string]int{"count": *req.Count})
	proxyFleetControl(w, r, http.MethodPut, "/control/count", body)
}

// GET /api/v1/admin/demobots/version reports the fleet's running commit (from
// its control API) next to the private repo's branch tip (from the updater
// sidecar, which holds the deploy key). Each half degrades independently.
func (h *AdminHandler) demobotsVersion(w http.ResponseWriter, r *http.Request) {
	if !fleetConfigured() {
		writeJSON(w, http.StatusOK, map[string]interface{}{"configured": false})
		return
	}
	payload := map[string]interface{}{
		"configured":        true,
		"updaterConfigured": config.C.UpdaterURL != "" && config.C.UpdaterSharedSecret != "",
	}

	// The running-commit half used to be re-fetched here from the fleet's
	// /control/status, sequentially before the updater call (worst case the
	// two upstream timeouts summed to ~20s). Its only consumer — the admin
	// panel's version card — already holds the running commit from the
	// /demobots/status call it makes first and never read this payload's
	// "running" key, so the redundant upstream fetch is gone; this endpoint
	// now answers within the single 12s updater bound.
	if latest, err := fetchUpdaterDemobotsLatest(r.Context()); err == nil {
		payload["latest"] = latest
	} else {
		payload["latestError"] = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}

// POST /api/v1/admin/demobots/update forwards a fleet-update request to the
// updater sidecar. Body: {"commitSha": "<full sha>"} or {} for the branch tip.
// Unlike the app self-update there is no maintenance notice: only the bot
// container restarts, the arena itself stays up.
func (h *AdminHandler) demobotsTriggerUpdate(w http.ResponseWriter, r *http.Request) {
	if config.C.UpdaterURL == "" || config.C.UpdaterSharedSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "updater not configured (ARENA_UPDATER_URL / ARENA_UPDATER_SHARED_SECRET unset)")
		return
	}
	var req struct {
		CommitSha string `json:"commitSha"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CommitSha != "" && !fullSHARe.MatchString(req.CommitSha) {
		writeError(w, http.StatusBadRequest, "commitSha must be a full 40-character lowercase commit SHA or omitted")
		return
	}

	target, err := updaterEndpointURL("/demobots/update")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid updater URL")
		return
	}
	body, _ := json.Marshal(map[string]string{"commitSha": req.CommitSha})
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build updater request")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	proxyReq.Header.Set("Authorization", "Bearer "+config.C.UpdaterSharedSecret)

	resp, err := updaterHTTPClient.Do(proxyReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "updater unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	var payload map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		writeError(w, http.StatusBadGateway, "updater returned invalid JSON")
		return
	}
	writeJSON(w, resp.StatusCode, payload)
}

// updaterEndpointURL rebases ARENA_UPDATER_URL (which points at the sidecar's
// /update endpoint) onto a sibling path like /demobots/update or
// /demobots/latest.
func updaterEndpointURL(path string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(config.C.UpdaterURL))
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(u.Path, "/")
	base = strings.TrimSuffix(base, "/update")
	u.Path = strings.TrimRight(base, "/") + path
	return u.String(), nil
}

func fetchUpdaterDemobotsLatest(ctx context.Context) (map[string]interface{}, error) {
	if config.C.UpdaterURL == "" || config.C.UpdaterSharedSecret == "" {
		return nil, errNoUpdater
	}
	target, err := updaterEndpointURL("/demobots/latest")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+config.C.UpdaterSharedSecret)
	resp, err := updaterHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := payload["error"].(string)
		if msg == "" {
			msg = resp.Status
		}
		return nil, &updaterError{message: msg}
	}
	return payload, nil
}

type updaterError struct{ message string }

func (e *updaterError) Error() string { return e.message }

var errNoUpdater = &updaterError{message: "updater not configured"}
