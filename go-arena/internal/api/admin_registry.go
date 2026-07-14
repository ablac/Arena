package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	demoTemplateNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{1,39}$`)
	hexColorRE         = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	customMapNameRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,39}$`)
)

type mapPreviewRequest struct {
	Shape string `json:"shape"`
	Seed  int64  `json:"seed"`
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
}

type mapPreviewResponse struct {
	Shape       string   `json:"shape"`
	Seed        int64    `json:"seed"`
	Cols        int      `json:"cols"`
	Rows        int      `json:"rows"`
	Terrain     []string `json:"terrain"`
	PlayablePct float64  `json:"playable_pct"`
}

type customMapPayload struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	BaseShape   string `json:"base_shape"`
	Seed        int64  `json:"seed"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

type contentBlockRecord struct {
	Key       string    `json:"key"`
	Label     string    `json:"label"`
	Value     string    `json:"value"`
	Published bool      `json:"published"`
	Updated   time.Time `json:"updated_at"`
}

func defaultContentBlocks() map[string]contentBlockRecord {
	now := time.Now().UTC()
	return map[string]contentBlockRecord{
		"home.hero.eyebrow":       {Key: "home.hero.eyebrow", Label: "Home hero eyebrow", Value: "Autonomous Combat Sandbox", Published: true, Updated: now},
		"home.hero.title":         {Key: "home.hero.title", Label: "Home hero title", Value: "AI BATTLE ARENA", Published: true, Updated: now},
		"home.hero.subtitle":      {Key: "home.hero.subtitle", Label: "Home hero subtitle", Value: "Deploy a bot, stream live combat, and iterate against a scoreboard that is always moving.", Published: true, Updated: now},
		"home.hero.cta.primary":   {Key: "home.hero.cta.primary", Label: "Primary CTA", Value: "Get Started", Published: true, Updated: now},
		"home.hero.cta.secondary": {Key: "home.hero.cta.secondary", Label: "Secondary CTA", Value: "Dashboard", Published: true, Updated: now},
		"home.announcement":       {Key: "home.announcement", Label: "Home announcement", Value: "Season controls, live maps, and demo bots are managed from the admin console.", Published: true, Updated: now},
		"bot-guide.notice":        {Key: "bot-guide.notice", Label: "Bot guide notice", Value: "Keep bot names clean, stats balanced, and strategies observable.", Published: true, Updated: now},
		"rules.banner":            {Key: "rules.banner", Label: "Rules banner", Value: "Fair play keeps the arena useful for every builder.", Published: true, Updated: now},
	}
}

func isMissingAdminRegistryTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01" || pgErr.Code == "42501"
	}
	return false
}

func buildMapPreview(req mapPreviewRequest) (mapPreviewResponse, error) {
	shape := strings.ToLower(strings.TrimSpace(req.Shape))
	if shape == "" {
		shape = config.C.MapShape
	}
	if shape == "random" {
		shape = "square"
	}
	if !game.IsKnownMapShape(shape) {
		return mapPreviewResponse{}, fmt.Errorf("unknown map shape %q", shape)
	}

	cols := clampInt(req.Cols, 16, 96)
	rows := clampInt(req.Rows, 16, 96)
	if cols == 0 {
		cols = 52
	}
	if rows == 0 {
		rows = 52
	}

	var mask [][]bool
	if req.Seed != 0 {
		mask = game.GenerateShapeMaskWithSeed(game.MapShape(shape), cols, rows, req.Seed)
	} else {
		mask = game.GenerateShapeMask(game.MapShape(shape), cols, rows)
	}

	terrain := make([]string, rows)
	playable := 0
	total := cols * rows
	for y := 0; y < rows; y++ {
		var b strings.Builder
		b.Grow(cols)
		for x := 0; x < cols; x++ {
			cellOpen := mask == nil || mask[x][y]
			if cellOpen {
				playable++
				b.WriteByte('.')
			} else {
				b.WriteByte('#')
			}
		}
		terrain[y] = b.String()
	}

	return mapPreviewResponse{
		Shape:       shape,
		Seed:        req.Seed,
		Cols:        cols,
		Rows:        rows,
		Terrain:     terrain,
		PlayablePct: float64(playable) * 100 / float64(total),
	}, nil
}

func (h *AdminHandler) loadAdminRegistries(ctx context.Context) {
	if db.Pool == nil {
		game.SetRandomShapePool(strings.Split(config.C.MapShapePool, ","))
		return
	}

	rows, err := db.Pool.Query(ctx, `SELECT name, display_name, base_shape, seed, enabled FROM custom_map_templates`)
	if err != nil {
		slog.Warn("failed to load custom map templates", "error", err)
		game.SetRandomShapePool(strings.Split(config.C.MapShapePool, ","))
		return
	}
	defer rows.Close()

	templates := make([]game.CustomMapTemplate, 0)
	for rows.Next() {
		var t game.CustomMapTemplate
		if err := rows.Scan(&t.Name, &t.DisplayName, &t.BaseShape, &t.Seed, &t.Enabled); err != nil {
			slog.Warn("failed to scan custom map template", "error", err)
			continue
		}
		templates = append(templates, t)
	}
	registerCustomMapsAndApplyPool(templates, config.C.MapShapePool)
}

func registerCustomMapsAndApplyPool(templates []game.CustomMapTemplate, pool string) {
	// Custom shapes must exist in the registry before normalization, otherwise
	// SetRandomShapePool silently drops a valid persisted custom:<slug> entry.
	for _, template := range templates {
		if template.Enabled {
			game.RegisterCustomMap(template)
		}
	}
	game.SetRandomShapePool(strings.Split(pool, ","))
}

func PublicContentBlocks(w http.ResponseWriter, r *http.Request) {
	records := defaultContentBlocks()
	if db.Pool != nil {
		rows, err := db.Pool.Query(r.Context(), `SELECT key, label, value, published, updated_at FROM admin_content_blocks WHERE published = true`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var rec contentBlockRecord
				if scanErr := rows.Scan(&rec.Key, &rec.Label, &rec.Value, &rec.Published, &rec.Updated); scanErr == nil {
					records[rec.Key] = rec
				}
			}
		} else {
			slog.Warn("failed to query public content blocks", "error", err)
		}
	}

	blocks := make(map[string]string, len(records))
	for key, rec := range records {
		if rec.Published {
			blocks[key] = rec.Value
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"blocks": blocks,
		"count":  len(blocks),
	})
}

func (h *AdminHandler) listContentBlocks(w http.ResponseWriter, r *http.Request) {
	records := defaultContentBlocks()
	if db.Pool != nil {
		rows, err := db.Pool.Query(r.Context(), `SELECT key, label, value, published, updated_at FROM admin_content_blocks ORDER BY key`)
		if err != nil {
			if isMissingAdminRegistryTable(err) {
				slog.Warn("admin content registry unavailable; using built-in defaults", "error", err)
				list := make([]contentBlockRecord, 0, len(records))
				for _, rec := range records {
					list = append(list, rec)
				}
				sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
				writeJSON(w, http.StatusOK, map[string]interface{}{"blocks": list, "count": len(list), "source": "built_in"})
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to query content blocks")
			return
		}
		defer rows.Close()
		for rows.Next() {
			var rec contentBlockRecord
			if err := rows.Scan(&rec.Key, &rec.Label, &rec.Value, &rec.Published, &rec.Updated); err == nil {
				records[rec.Key] = rec
			}
		}
	}

	list := make([]contentBlockRecord, 0, len(records))
	for _, rec := range records {
		list = append(list, rec)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	writeJSON(w, http.StatusOK, map[string]interface{}{"blocks": list, "count": len(list)})
}

func (h *AdminHandler) updateContentBlock(w http.ResponseWriter, r *http.Request) {
	if db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	defaults := defaultContentBlocks()
	defaultRec, known := defaults[key]
	if !known {
		writeError(w, http.StatusBadRequest, "unknown content block")
		return
	}

	var req struct {
		Label     string `json:"label"`
		Value     string `json:"value"`
		Published *bool  `json:"published,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	value := strings.TrimSpace(req.Value)
	if len(value) > 1200 {
		writeError(w, http.StatusBadRequest, "content value is too long")
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = defaultRec.Label
	}
	published := defaultRec.Published
	if req.Published != nil {
		published = *req.Published
	}

	var rec contentBlockRecord
	err := db.Pool.QueryRow(r.Context(), `
		INSERT INTO admin_content_blocks (key, label, value, published, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (key) DO UPDATE
		SET label = EXCLUDED.label, value = EXCLUDED.value, published = EXCLUDED.published, updated_at = NOW()
		RETURNING key, label, value, published, updated_at`,
		key, label, value, published,
	).Scan(&rec.Key, &rec.Label, &rec.Value, &rec.Published, &rec.Updated)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save content block")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (h *AdminHandler) getMapSettings(w http.ResponseWriter, r *http.Request) {
	h.overrideMu.Lock()
	defer h.overrideMu.Unlock()
	desired, _, desiredErr := h.desiredGameConfigLocked()
	c := &desired
	customMaps := game.ListCustomMaps()
	if db.Pool != nil {
		rows, err := db.Pool.Query(r.Context(), `SELECT name, display_name, base_shape, seed, enabled FROM custom_map_templates ORDER BY name`)
		if err == nil {
			defer rows.Close()
			customMaps = []game.CustomMapTemplate{}
			for rows.Next() {
				var t game.CustomMapTemplate
				if err := rows.Scan(&t.Name, &t.DisplayName, &t.BaseShape, &t.Seed, &t.Enabled); err == nil {
					customMaps = append(customMaps, t)
				}
			}
		}
	}
	if customMaps == nil {
		customMaps = []game.CustomMapTemplate{}
	}
	response := map[string]interface{}{
		"built_in_shapes":       game.BuiltInMapShapeNames(),
		"custom_maps":           customMaps,
		"enabled_shapes":        mapShapePoolNames(c.MapShapePool),
		"map_shape":             c.MapShape,
		"map_shape_pool":        c.MapShapePool,
		"obstacle_count_min":    c.ObstacleCountMin,
		"obstacle_count_max":    c.ObstacleCountMax,
		"arena_size_dynamic":    c.ArenaSizeDynamic,
		"arena_size_base_bots":  c.ArenaSizeBaseBots,
		"arena_size_max_bots":   c.ArenaSizeMaxBots,
		"arena_size_min_scale":  c.ArenaSizeMinScale,
		"arena_size_max_scale":  c.ArenaSizeMaxScale,
		"zone_initial_radius":   c.ZoneInitialRadius,
		"zone_cover_map":        c.ZoneCoverMap,
		"zone_shrink_pct":       c.ZoneShrinkPercent,
		"zone_shrink_interval":  c.ZoneShrinkInterval,
		"zone_shrink_delay":     c.ZoneShrinkDelay,
		"zone_damage":           c.ZoneDamagePerTick,
		"zone_min_radius":       c.ZoneMinRadius,
		"round_modifier_chance": c.RoundModifierChance,
	}
	configResponse := h.gameConfigResponseLocked()
	response["_persistence"] = configResponse["_persistence"]
	if desiredErr != nil {
		response["error"] = "stored map overrides could not be validated"
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *AdminHandler) updateMapSettings(w http.ResponseWriter, r *http.Request) {
	if !h.adminOverridePersistenceAvailable() {
		writeError(w, http.StatusServiceUnavailable, "database is required to save map settings")
		return
	}
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	h.overrideMu.Lock()
	defer h.overrideMu.Unlock()
	staged := cloneOverrideValues(updates)
	extraRejected := make([]string, 0, 1)
	if raw, ok := updates["enabled_shapes"]; ok {
		if list, ok := stringSlice(raw); ok {
			canonical, _ := canonicalMapShapePool(list)
			staged["map_shape_pool"] = canonical
		} else {
			extraRejected = append(extraRejected, "enabled_shapes")
		}
		delete(staged, "enabled_shapes")
	}
	applied, rejected, restartRequired, err := h.stageGameConfigUpdatesLocked(r.Context(), staged)
	if err != nil {
		slog.Error("failed to persist admin map settings", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist map settings; active settings were not changed")
		return
	}
	rejected = append(rejected, extraRejected...)
	sort.Strings(rejected)
	if len(applied) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": "no valid map settings were supplied", "rejected": rejected,
		})
		return
	}
	message := "map settings saved; values are already active"
	if restartRequired {
		message = "map settings saved; restart the server to activate them"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":          message,
		"applied":          applied,
		"rejected":         rejected,
		"persisted":        true,
		"restart_required": restartRequired,
		"activation":       "server_restart",
	})
}

func (h *AdminHandler) previewMap(w http.ResponseWriter, r *http.Request) {
	var req mapPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prev, err := buildMapPreview(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, prev)
}

func (h *AdminHandler) upsertCustomMap(w http.ResponseWriter, r *http.Request) {
	if db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}
	var req customMapPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if param := strings.TrimSpace(chi.URLParam(r, "name")); param != "" {
		req.Name = strings.ToLower(param)
	}
	t, err := validateCustomMap(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err = db.Pool.QueryRow(r.Context(), `
		INSERT INTO custom_map_templates (name, display_name, base_shape, seed, enabled, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (name) DO UPDATE
		SET display_name = EXCLUDED.display_name, base_shape = EXCLUDED.base_shape,
		    seed = EXCLUDED.seed, enabled = EXCLUDED.enabled, updated_at = NOW()
		RETURNING name, display_name, base_shape, seed, enabled`,
		t.Name, t.DisplayName, t.BaseShape, t.Seed, t.Enabled,
	).Scan(&t.Name, &t.DisplayName, &t.BaseShape, &t.Seed, &t.Enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save custom map")
		return
	}
	if t.Enabled {
		game.RegisterCustomMap(t)
	} else {
		game.RemoveCustomMap(t.Name)
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *AdminHandler) deleteCustomMap(w http.ResponseWriter, r *http.Request) {
	if db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "database not available")
		return
	}
	name := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "name")))
	tag, err := db.Pool.Exec(r.Context(), `DELETE FROM custom_map_templates WHERE name = $1`, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete custom map")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "custom map not found")
		return
	}
	game.RemoveCustomMap(name)
	writeJSON(w, http.StatusOK, map[string]interface{}{"message": "custom map deleted", "name": name})
}

func validateCustomMap(req customMapPayload) (game.CustomMapTemplate, error) {
	req.Name = strings.ToLower(strings.TrimSpace(req.Name))
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.BaseShape = strings.ToLower(strings.TrimSpace(req.BaseShape))
	if !customMapNameRE.MatchString(req.Name) {
		return game.CustomMapTemplate{}, errors.New("map name must be a 2-40 character lowercase slug")
	}
	if req.DisplayName == "" || len(req.DisplayName) > 60 {
		return game.CustomMapTemplate{}, errors.New("display name is required and must be 60 characters or less")
	}
	if !game.IsBuiltInMapShape(req.BaseShape) {
		return game.CustomMapTemplate{}, errors.New("base shape must be a built-in map shape")
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return game.CustomMapTemplate{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		BaseShape:   req.BaseShape,
		Seed:        req.Seed,
		Enabled:     enabled,
	}, nil
}

// applyGameConfigUpdatesTo validates updates against an immutable base
// snapshot. It never publishes the candidate to config.C, which makes it safe
// for admin requests to validate restart-staged values while the engine keeps
// reading its active startup configuration.
func applyGameConfigUpdatesTo(base config.Config, updates map[string]interface{}) (config.Config, map[string]interface{}) {
	c := &base
	applied := make(map[string]interface{})
	handled := make(map[string]bool)

	// Coupled bounds must be validated and applied together. Iterating the JSON
	// object directly is nondeterministic, and validating the second value
	// against a possibly updated first value caused identical requests to be
	// only half-applied depending on Go's map iteration order.
	if min, max, present, valid := intConfigPair(updates, "obstacle_count_min", "obstacle_count_max", 0); present {
		handled["obstacle_count_min"] = true
		handled["obstacle_count_max"] = true
		if valid {
			c.ObstacleCountMin, c.ObstacleCountMax = min, max
			applied["obstacle_count_min"], applied["obstacle_count_max"] = min, max
		}
	}
	if min, max, present, valid := intConfigPair(updates, "arena_size_base_bots", "arena_size_max_bots", 1); present {
		handled["arena_size_base_bots"] = true
		handled["arena_size_max_bots"] = true
		if valid {
			c.ArenaSizeBaseBots, c.ArenaSizeMaxBots = min, max
			applied["arena_size_base_bots"], applied["arena_size_max_bots"] = min, max
		}
	}
	if min, max, present, valid := floatConfigPair(updates, "arena_size_min_scale", "arena_size_max_scale"); present {
		handled["arena_size_min_scale"] = true
		handled["arena_size_max_scale"] = true
		if valid {
			c.ArenaSizeMinScale, c.ArenaSizeMaxScale = min, max
			applied["arena_size_min_scale"], applied["arena_size_max_scale"] = min, max
		}
	}

	for key, val := range updates {
		if handled[key] {
			continue
		}
		switch key {
		case "tick_rate":
			if v, ok := toInt(val); ok && v >= 1 && v <= 60 {
				c.TickRate = v
				applied[key] = v
			}
		case "max_bots":
			if v, ok := toInt(val); ok && v >= 1 {
				c.MaxBots = v
				applied[key] = v
			}
		case "max_spectators":
			if v, ok := toInt(val); ok && v >= 0 {
				c.MaxSpectators = v
				applied[key] = v
			}
		case "arena_width":
			if v, ok := toFloat(val); ok && v > 0 {
				c.ArenaWidth = v
				applied[key] = v
			}
		case "arena_height":
			if v, ok := toFloat(val); ok && v > 0 {
				c.ArenaHeight = v
				applied[key] = v
			}
		case "round_duration":
			if v, ok := toFloat(val); ok && v >= 10 {
				c.RoundDuration = v
				applied[key] = v
			}
		case "intermission_time":
			if v, ok := toFloat(val); ok && v >= 1 {
				c.IntermissionTime = v
				applied[key] = v
			}
		case "lobby_countdown":
			if v, ok := toFloat(val); ok && v >= 1 {
				c.LobbyCountdown = v
				applied[key] = v
			}
		case "min_bots_to_start":
			if v, ok := toInt(val); ok && v >= 1 {
				c.MinBotsToStart = v
				applied[key] = v
			}
		case "stat_budget":
			if v, ok := toInt(val); ok && v >= 1 {
				c.StatBudget = v
				applied[key] = v
			}
		case "game_mode":
			if v, ok := val.(string); ok && stringIn(v, []string{"ffa", "team_battle", "ctf"}) {
				c.GameModeName = v
				applied[key] = v
			}
		case "team_count":
			if v, ok := toInt(val); ok && v >= 2 && v <= 8 {
				c.TeamCount = v
				applied[key] = v
			}
		case "friendly_fire":
			if v, ok := val.(bool); ok {
				c.FriendlyFire = v
				applied[key] = v
			}
		case "map_shape":
			if v, ok := val.(string); ok {
				v = strings.ToLower(strings.TrimSpace(v))
				if validPersistedMapShape(v) {
					c.MapShape = v
					applied[key] = v
				}
			}
		case "map_shape_pool":
			if canonical, ok := canonicalMapShapePool(val); ok {
				c.MapShapePool = canonical
				// Persist the same scalar shape that the startup loader accepts.
				// Older releases stored []string here, so canonicalMapShapePool also
				// accepts arrays when reading legacy rows.
				applied[key] = canonical
			}
		case "zone_damage":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.ZoneDamagePerTick = v
				applied[key] = v
			}
		case "zone_shrink_pct":
			if v, ok := toFloat(val); ok && v >= 0 && v <= 1 {
				c.ZoneShrinkPercent = v
				applied[key] = v
			}
		case "zone_shrink_interval":
			if v, ok := toFloat(val); ok && v >= 1 {
				c.ZoneShrinkInterval = v
				applied[key] = v
			}
		case "zone_min_radius":
			if v, ok := toFloat(val); ok && v >= 20 {
				c.ZoneMinRadius = v
				applied[key] = v
			}
		case "zone_shrink_delay":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.ZoneShrinkDelay = v
				applied[key] = v
			}
		case "zone_initial_radius":
			if v, ok := toFloat(val); ok && v >= 20 {
				c.ZoneInitialRadius = v
				applied[key] = v
			}
		case "zone_cover_map":
			if v, ok := val.(bool); ok {
				c.ZoneCoverMap = v
				applied[key] = v
			}
		case "obstacle_count_min":
			if v, ok := toInt(val); ok && v >= 0 && v <= c.ObstacleCountMax {
				c.ObstacleCountMin = v
				applied[key] = v
			}
		case "obstacle_count_max":
			if v, ok := toInt(val); ok && v >= c.ObstacleCountMin {
				c.ObstacleCountMax = v
				applied[key] = v
			}
		case "arena_size_dynamic":
			if v, ok := val.(bool); ok {
				c.ArenaSizeDynamic = v
				applied[key] = v
			}
		case "arena_size_base_bots":
			if v, ok := toInt(val); ok && v >= 1 {
				c.ArenaSizeBaseBots = v
				applied[key] = v
			}
		case "arena_size_max_bots":
			if v, ok := toInt(val); ok && v >= c.ArenaSizeBaseBots {
				c.ArenaSizeMaxBots = v
				applied[key] = v
			}
		case "arena_size_min_scale":
			if v, ok := toFloat(val); ok && v > 0 && v <= c.ArenaSizeMaxScale {
				c.ArenaSizeMinScale = v
				applied[key] = v
			}
		case "arena_size_max_scale":
			if v, ok := toFloat(val); ok && v >= c.ArenaSizeMinScale {
				c.ArenaSizeMaxScale = v
				applied[key] = v
			}
		case "round_modifier_chance":
			if v, ok := toFloat(val); ok && v >= 0 && v <= 1 {
				c.RoundModifierChance = v
				applied[key] = v
			}
		case "afk_timeout_ticks":
			if v, ok := toInt(val); ok && v >= 0 {
				c.AFKTimeoutTicks = v
				applied[key] = v
			}
		case "stat_hp_base":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.StatHPBase = v
				applied[key] = v
			}
		case "stat_hp_per_point":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.StatHPPerPoint = v
				applied[key] = v
			}
		case "stat_speed_base":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.StatSpeedBase = v
				applied[key] = v
			}
		case "stat_speed_per_point":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.StatSpeedPerPoint = v
				applied[key] = v
			}
		case "stat_attack_base":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.StatAttackBase = v
				applied[key] = v
			}
		case "stat_attack_per_point":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.StatAttackPerPoint = v
				applied[key] = v
			}
		case "stat_defense_per_point":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.StatDefensePerPoint = v
				applied[key] = v
			}
		case "dodge_speed_mult":
			if v, ok := toFloat(val); ok && v >= 0 {
				c.DodgeSpeedMult = v
				applied[key] = v
			}
		case "dodge_invuln_ticks":
			if v, ok := toInt(val); ok && v >= 0 {
				c.DodgeInvulnTicks = v
				applied[key] = v
			}
		case "dodge_cooldown_ticks":
			if v, ok := toInt(val); ok && v >= 0 {
				c.DodgeCooldownTicks = v
				applied[key] = v
			}
		case "projectile_speed":
			if v, ok := toFloat(val); ok && v > 0 {
				c.ProjectileSpeed = v
				applied[key] = v
			}
		}
	}
	return base, applied
}

// applyGameConfigUpdates is retained for startup-oriented helpers and focused
// validation tests. Live admin handlers use applyGameConfigUpdatesTo and stage
// the result for restart instead of mutating config.C.
func applyGameConfigUpdates(updates map[string]interface{}) map[string]interface{} {
	candidate, applied := applyGameConfigUpdatesTo(config.C, updates)
	config.C = candidate
	return applied
}

func canonicalMapShapePool(value interface{}) (string, bool) {
	var names []string
	switch typed := value.(type) {
	case string:
		names = strings.Split(typed, ",")
	default:
		var ok bool
		names, ok = stringSlice(value)
		if !ok {
			return "", false
		}
	}

	seen := make(map[string]bool)
	canonical := make([]string, 0, len(names))
	for _, raw := range names {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" || name == "random" || seen[name] {
			continue
		}
		if !game.IsBuiltInMapShape(name) && !validCustomMapShapeName(name) {
			continue
		}
		seen[name] = true
		canonical = append(canonical, name)
	}
	if len(canonical) == 0 {
		canonical = []string{"square"}
	}
	return strings.Join(canonical, ","), true
}

func validPersistedMapShape(name string) bool {
	return name == "random" || game.IsKnownMapShape(name) || validCustomMapShapeName(name)
}

func validCustomMapShapeName(name string) bool {
	if !strings.HasPrefix(name, "custom:") {
		return false
	}
	return customMapNameRE.MatchString(strings.TrimPrefix(name, "custom:"))
}

func intConfigPair(updates map[string]interface{}, minKey, maxKey string, lowerBound int) (min, max int, present, valid bool) {
	minRaw, hasMin := updates[minKey]
	maxRaw, hasMax := updates[maxKey]
	if !hasMin || !hasMax {
		return 0, 0, false, false
	}
	min, minOK := toInt(minRaw)
	max, maxOK := toInt(maxRaw)
	return min, max, true, minOK && maxOK && min >= lowerBound && max >= min
}

func floatConfigPair(updates map[string]interface{}, minKey, maxKey string) (min, max float64, present, valid bool) {
	minRaw, hasMin := updates[minKey]
	maxRaw, hasMax := updates[maxKey]
	if !hasMin || !hasMax {
		return 0, 0, false, false
	}
	min, minOK := toFloat(minRaw)
	max, maxOK := toFloat(maxRaw)
	return min, max, true, minOK && maxOK && min > 0 && max >= min
}

func rejectedConfigKeys(updates, applied map[string]interface{}) []string {
	rejected := make([]string, 0)
	for key := range updates {
		if _, ok := applied[key]; !ok {
			rejected = append(rejected, key)
		}
	}
	sort.Strings(rejected)
	return rejected
}

func cloneStats(stats map[string]int) map[string]int {
	out := make(map[string]int, len(stats))
	for k, v := range stats {
		out[k] = v
	}
	return out
}

func stringIn(v string, options []string) bool {
	for _, option := range options {
		if v == option {
			return true
		}
	}
	return false
}

func clampInt(v, min, max int) int {
	if v == 0 {
		return 0
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func stringSlice(v interface{}) ([]string, bool) {
	switch typed := v.(type) {
	case []string:
		return typed, true
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}
