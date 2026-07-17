package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"arena-server/internal/db"
)

// The demo bot fleet runs as an external private process speaking the public
// bot protocol. Identity (key + name + color) is fully self-service through
// the public API; cosmetics are the one privileged step, granted here so the
// fleet can showcase shop items without shipping purchase entitlements.

type demoLoadoutRequest struct {
	BotID     string `json:"bot_id"`
	PackID    string `json:"pack_id"`
	BotSkinID string `json:"bot_skin_id"`
	TrailID   string `json:"trail_id"`
}

type cosmeticSelection struct {
	Slot       string `json:"slot"`
	CosmeticID string `json:"cosmetic_id"`
}

// applyDemoLoadout handles POST /api/v1/admin/bots/demo-loadout: grant and
// equip one complete cosmetic pack and/or one paid trail on a bot.
func (h *AdminHandler) applyDemoLoadout(w http.ResponseWriter, r *http.Request) {
	var req demoLoadoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BotID == "" {
		writeError(w, http.StatusBadRequest, "bot_id is required")
		return
	}
	if req.PackID == "" && req.BotSkinID == "" && req.TrailID == "" {
		writeError(w, http.StatusBadRequest, "pack_id, bot_skin_id, or trail_id is required")
		return
	}

	catalog, err := db.GetPublicCosmeticCatalog(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "cosmetic catalog unavailable")
		return
	}

	selections, err := cosmeticSelectionsForDemoLoadout(*catalog, req.PackID, req.BotSkinID, req.TrailID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	for _, selection := range selections {
		if _, err := db.GrantCosmeticEntitlement(r.Context(), req.BotID, selection.CosmeticID, "demo", ""); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("grant %s failed", selection.CosmeticID))
			return
		}
	}
	for _, selection := range selections {
		if _, err := db.EquipCosmetic(r.Context(), req.BotID, selection.Slot, selection.CosmeticID); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("equip %s failed", selection.CosmeticID))
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bot_id":     req.BotID,
		"selections": selections,
	})
}

func cosmeticSelectionsForDemoLoadout(catalog db.CosmeticCatalog, packID, botSkinID, trailID string) ([]cosmeticSelection, error) {
	selections := make([]cosmeticSelection, 0, 4)
	if packID != "" {
		packSelections, err := cosmeticSelectionsForPack(catalog, packID)
		if err != nil {
			return nil, err
		}
		selections = append(selections, packSelections...)
	}
	if botSkinID != "" {
		botSkinSelection, err := cosmeticSelectionForBotSkin(catalog, botSkinID)
		if err != nil {
			return nil, err
		}
		replaced := false
		for index := range selections {
			if selections[index].Slot == db.CosmeticSlotBotSkin {
				selections[index] = botSkinSelection
				replaced = true
				break
			}
		}
		if !replaced {
			selections = append(selections, botSkinSelection)
		}
	}
	if trailID != "" {
		trailSelection, err := cosmeticSelectionForTrail(catalog, trailID)
		if err != nil {
			return nil, err
		}
		selections = append(selections, trailSelection)
	}
	return selections, nil
}

func cosmeticSelectionForBotSkin(catalog db.CosmeticCatalog, cosmeticID string) (cosmeticSelection, error) {
	for _, item := range catalog.Items {
		if item.ID != cosmeticID {
			continue
		}
		if item.Slot != db.CosmeticSlotBotSkin || item.AssetKey == "standard" || item.IsFree || !item.IsActive {
			return cosmeticSelection{}, fmt.Errorf("cosmetic bot skin %q is not an active paid bot skin", cosmeticID)
		}
		return cosmeticSelection{Slot: db.CosmeticSlotBotSkin, CosmeticID: item.ID}, nil
	}
	return cosmeticSelection{}, fmt.Errorf("cosmetic bot skin %q was not found", cosmeticID)
}

func cosmeticSelectionForTrail(catalog db.CosmeticCatalog, cosmeticID string) (cosmeticSelection, error) {
	for _, item := range catalog.Items {
		if item.ID != cosmeticID {
			continue
		}
		if item.Slot != db.CosmeticSlotTrail || item.CategoryID != db.CosmeticTrailCategoryID ||
			item.AssetKey == "standard" || !item.IsActive {
			return cosmeticSelection{}, fmt.Errorf("cosmetic trail %q is not an active paid trail", cosmeticID)
		}
		return cosmeticSelection{Slot: db.CosmeticSlotTrail, CosmeticID: item.ID}, nil
	}
	return cosmeticSelection{}, fmt.Errorf("cosmetic trail %q was not found", cosmeticID)
}

func cosmeticSelectionsForPack(catalog db.CosmeticCatalog, packID string) ([]cosmeticSelection, error) {
	for _, pack := range catalog.Packs {
		if pack.ID != packID {
			continue
		}
		bySlot := make(map[string]string, len(pack.Items))
		for _, item := range pack.Items {
			if !db.IsValidCosmeticSlot(item.Slot) || bySlot[item.Slot] != "" {
				return nil, fmt.Errorf("cosmetic pack %q does not contain one item per slot", packID)
			}
			bySlot[item.Slot] = item.ID
		}
		selections := make([]cosmeticSelection, 0, 3)
		for _, slot := range []string{db.CosmeticSlotBotSkin, db.CosmeticSlotWeaponSkin, db.CosmeticSlotAttachment} {
			cosmeticID := bySlot[slot]
			if cosmeticID == "" {
				return nil, fmt.Errorf("cosmetic pack %q is missing slot %q", packID, slot)
			}
			selections = append(selections, cosmeticSelection{Slot: slot, CosmeticID: cosmeticID})
		}
		if len(pack.Items) != len(selections) {
			return nil, fmt.Errorf("cosmetic pack %q contains unsupported extra items", packID)
		}
		return selections, nil
	}
	return nil, fmt.Errorf("cosmetic pack %q was not found", packID)
}
