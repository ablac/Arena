package api

// Self-update: version comparison + browser-triggered "update to latest".
//
// The running server never holds Docker-socket access. Instead, a separate
// arena-updater sidecar (updater/, holding /var/run/docker.sock) does the
// fetch+build+recreate. These admin endpoints (1) report the running commit vs.
// the latest commit on the release branch (from GitHub's public API), and (2)
// forward an operator's "update to <sha>" request to the sidecar over the
// internal compose network, authenticated with a shared secret. All three live
// under the admin router, so they inherit its X-Admin-Token / OIDC auth.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/version"
)

var fullSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// githubHTTPClient is a shared client with a sane timeout for GitHub API calls.
var githubHTTPClient = &http.Client{Timeout: 12 * time.Second}

// updaterHTTPClient is intentionally separate from githubHTTPClient because the
// sidecar is a local control-plane dependency with different semantics.
var updaterHTTPClient = &http.Client{Timeout: 15 * time.Second}

// versionInfoCache memoizes the GitHub-derived comparison for a short window so
// every Admin-Panel load does not hit the GitHub API (and its 60/hr anonymous
// rate limit).
var versionInfoCache struct {
	mu        sync.Mutex
	payload   map[string]interface{}
	expiresAt time.Time
}

const versionCacheTTL = 60 * time.Second

// splitRepo parses "owner/repo" into its parts, falling back to ablac/Arena.
func splitRepo(repo string) (owner, name string) {
	parts := strings.SplitN(strings.TrimSpace(repo), "/", 2)
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0], parts[1]
	}
	return "ablac", "Arena"
}

// adminVersionInfo reports the running commit alongside the latest commit on the
// configured release branch and how many commits behind the running build is.
func adminVersionInfo(w http.ResponseWriter, r *http.Request) {
	versionInfoCache.mu.Lock()
	if versionInfoCache.payload != nil && time.Now().Before(versionInfoCache.expiresAt) {
		payload := versionInfoCache.payload
		versionInfoCache.mu.Unlock()
		writeJSON(w, http.StatusOK, payload)
		return
	}
	versionInfoCache.mu.Unlock()

	owner, repo := splitRepo(config.C.UpdateRepo)
	branch := config.C.UpdateBranch
	if branch == "" {
		branch = "main"
	}
	running := version.ResolvedCommit()

	payload := map[string]interface{}{
		"running": map[string]interface{}{
			"commit":    running,
			"short":     version.ShortCommit(),
			"buildTime": version.BuildTime,
		},
		"repo":              owner + "/" + repo,
		"branch":            branch,
		"updaterConfigured": config.C.UpdaterURL != "" && config.C.UpdaterSharedSecret != "",
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	latest, err := githubLatestCommit(ctx, owner, repo, branch, config.C.UpdateGitHubToken)
	if err != nil {
		// Degrade gracefully: still report the running commit so the card renders,
		// just without the "latest / N behind" comparison.
		slog.Warn("admin version: failed to fetch latest commit", "error", err)
		payload["latestError"] = err.Error()
		writeJSON(w, http.StatusOK, payload)
		return
	}
	payload["latest"] = latest

	latestSHA, _ := latest["commit"].(string)
	if running != "" && running != "unknown" && latestSHA != "" {
		if running == latestSHA {
			payload["commitsBehind"] = 0
		} else if behind, compareURL, err := githubCompare(ctx, owner, repo, running, latestSHA, config.C.UpdateGitHubToken); err == nil {
			payload["commitsBehind"] = behind
			payload["compareUrl"] = compareURL
		} else {
			slog.Warn("admin version: compare failed", "error", err)
		}
	}

	versionInfoCache.mu.Lock()
	versionInfoCache.payload = payload
	versionInfoCache.expiresAt = time.Now().Add(versionCacheTTL)
	versionInfoCache.mu.Unlock()

	writeJSON(w, http.StatusOK, payload)
}

// githubLatestCommit fetches the tip commit of a branch on a public repo.
func githubLatestCommit(ctx context.Context, owner, repo, branch, token string) (map[string]interface{}, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", owner, repo, branch)
	var body struct {
		SHA     string `json:"sha"`
		HTMLURL string `json:"html_url"`
		Commit  struct {
			Message   string `json:"message"`
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := githubGetJSON(ctx, url, token, &body); err != nil {
		return nil, err
	}
	short := body.SHA
	if len(short) > 7 {
		short = short[:7]
	}
	message := body.Commit.Message
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		message = message[:i]
	}
	return map[string]interface{}{
		"commit":      body.SHA,
		"short":       short,
		"committedAt": body.Commit.Committer.Date,
		"htmlUrl":     body.HTMLURL,
		"message":     message,
	}, nil
}

// githubCompare returns how many commits `base` is behind `head` on a public
// repo, plus the compare URL.
func githubCompare(ctx context.Context, owner, repo, base, head, token string) (int, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/compare/%s...%s", owner, repo, base, head)
	var body struct {
		BehindBy int    `json:"behind_by"`
		HTMLURL  string `json:"html_url"`
	}
	if err := githubGetJSON(ctx, url, token, &body); err != nil {
		return 0, "", err
	}
	return body.BehindBy, body.HTMLURL, nil
}

func githubGetJSON(ctx context.Context, url, token string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "arena-server")
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	resp, err := githubHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("github %s returned %d: %s", url, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func updaterStatusURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if strings.HasSuffix(u.Path, "/update") {
		u.Path = strings.TrimSuffix(u.Path, "/update")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Path == "" {
		u.Path = "/status"
	} else {
		u.Path += "/status"
	}
	return u.String(), nil
}

// triggerUpdate forwards an "update to <commitSha>" request to the arena-updater
// sidecar. It returns as soon as the sidecar accepts the job (202); progress is
// polled via updateStatus, since arena-server itself gets recreated as the last
// step and cannot deliver a response to a request it is holding.
func triggerUpdate(w http.ResponseWriter, r *http.Request) {
	if config.C.UpdaterURL == "" || config.C.UpdaterSharedSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "updater not configured (ARENA_UPDATER_URL / ARENA_UPDATER_SHARED_SECRET unset)")
		return
	}

	var reqBody struct {
		CommitSha string `json:"commitSha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !fullSHARe.MatchString(reqBody.CommitSha) {
		writeError(w, http.StatusBadRequest, "commitSha must be a full 40-character lowercase commit SHA")
		return
	}

	payload, _ := json.Marshal(map[string]string{
		"commitSha":   reqBody.CommitSha,
		"githubToken": config.C.UpdateGitHubToken, // optional; empty is fine for a public repo
	})

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.C.UpdaterURL, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build updater request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.C.UpdaterSharedSecret)

	resp, err := updaterHTTPClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to reach updater: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		writeError(w, http.StatusBadGateway, fmt.Sprintf("updater responded %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet))))
		return
	}

	slog.Warn("admin triggered self-update", "commit", reqBody.CommitSha)
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"ok": true, "commitSha": reqBody.CommitSha})
}

// updateStatus proxies the sidecar's in-memory progress. It always returns 200:
// an unreachable updater is itself meaningful ("likely mid recreate"), not an
// error, so the Admin-Panel overlay can keep polling through the brief window
// where arena-server recreates its own container.
func updateStatus(w http.ResponseWriter, r *http.Request) {
	unreachable := map[string]interface{}{"reachable": false, "inProgress": false}

	if config.C.UpdaterURL == "" || config.C.UpdaterSharedSecret == "" {
		writeJSON(w, http.StatusOK, unreachable)
		return
	}
	statusURL, err := updaterStatusURL(config.C.UpdaterURL)
	if err != nil {
		writeJSON(w, http.StatusOK, unreachable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, unreachable)
		return
	}
	req.Header.Set("Authorization", "Bearer "+config.C.UpdaterSharedSecret)

	resp, err := updaterHTTPClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusOK, unreachable)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusOK, unreachable)
		return
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		writeJSON(w, http.StatusOK, unreachable)
		return
	}
	status["reachable"] = true
	writeJSON(w, http.StatusOK, status)
}
