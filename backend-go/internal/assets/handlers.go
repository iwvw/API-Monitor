package assets

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listAssets(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	filter := assetFilter{
		Category:       strings.TrimSpace(query.Get("category")),
		AssetType:      strings.TrimSpace(query.Get("asset_type")),
		Status:         strings.TrimSpace(query.Get("status")),
		Provider:       strings.TrimSpace(query.Get("provider")),
		Tag:            strings.TrimSpace(query.Get("tag")),
		Query:          strings.TrimSpace(query.Get("q")),
		ExpiringWithin: intValue(query.Get("expiring_within"), 0),
		Limit:          boundedLimit(query.Get("limit")),
		Offset:         boundedOffset(query.Get("offset")),
	}
	assets, err := s.LoadAssets(r.Context(), filter)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, assets)
}

func (s *Service) getAsset(w http.ResponseWriter, r *http.Request, id string) {
	asset, ok, err := s.LoadAsset(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		response.Error(w, http.StatusNotFound, "asset not found")
		return
	}
	response.OK(w, asset)
}

func (s *Service) createAsset(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	asset, err := s.CreateAsset(r.Context(), payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.OK(w, asset)
}

func (s *Service) updateAsset(w http.ResponseWriter, r *http.Request, id string) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	if err := s.UpdateAsset(r.Context(), id, payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "asset not found")
			return
		}
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	asset, ok, err := s.LoadAsset(r.Context(), id)
	if err != nil || !ok {
		response.Error(w, http.StatusInternalServerError, "asset reload failed")
		return
	}
	response.OK(w, asset)
}

func (s *Service) deleteAsset(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.DeleteAsset(r.Context(), id); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Service) listEvents(w http.ResponseWriter, r *http.Request, id string) {
	limit := boundedLimit(r.URL.Query().Get("limit"))
	events, err := s.LoadEvents(r.Context(), id, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, events)
}

func (s *Service) listAlerts(w http.ResponseWriter, r *http.Request, id string) {
	alerts, err := s.LoadAlerts(r.Context(), id)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, alerts)
}

func (s *Service) candidates(w http.ResponseWriter, r *http.Request) {
	groups, err := s.LoadCandidates(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, groups)
}

func (s *Service) linkAssets(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Links []LinkInput `json:"links"`
	}
	if !decodeJSON(w, r, &payload) {
		return
	}
	results, err := s.LinkAssets(r.Context(), payload.Links)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.OK(w, results)
}

func (s *Service) refreshAsset(w http.ResponseWriter, r *http.Request, id string) {
	asset, err := s.RefreshAsset(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.Error(w, http.StatusNotFound, "asset not found")
			return
		}
		status := http.StatusInternalServerError
		if errors.Is(err, errInvalidInput) {
			status = http.StatusBadRequest
		}
		response.Error(w, status, err.Error())
		return
	}
	response.OK(w, asset)
}

func (s *Service) refreshAll(w http.ResponseWriter, r *http.Request) {
	success, failed, err := s.RefreshAllLinked(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, map[string]int{"refreshed": success, "failed": failed})
}

func (s *Service) scanExpiry(w http.ResponseWriter, r *http.Request) {
	s.RunExpiryScan(r.Context())
	response.OK(w, map[string]bool{"success": true})
}

func (s *Service) overview(w http.ResponseWriter, r *http.Request) {
	assets, settings, err := s.loadAllForStats(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	result := Overview{
		Costs:          aggregateCosts(assets),
		TotalMonthly:   map[string]float64{},
		TypeStats:      []TypeStat{},
		RecentExpiring: []Asset{},
	}
	typeAccumulator := map[string]*TypeStat{}
	for _, asset := range assets {
		if asset.Category == categoryPhysical {
			result.PhysicalCount++
		} else if asset.Category == categoryVirtual {
			result.VirtualCount++
		}
		switch asset.DerivedStatus {
		case statusExpiring:
			result.ExpiringCount++
		case statusExpired:
			result.ExpiredCount++
		case statusOrphan:
			result.OrphanCount++
		}
		switch bucketFor(asset) {
		case "expired":
			result.Buckets.Expired++
		case "within_7":
			result.Buckets.Within7++
		case "within_30":
			result.Buckets.Within30++
		case "no_renew":
			result.Buckets.NoRenew++
		default:
			result.Buckets.Normal++
		}

		stat, ok := typeAccumulator[asset.AssetType]
		if !ok {
			stat = &TypeStat{AssetType: asset.AssetType, Category: asset.Category, MonthlyByCcy: []CurrencyCost{}}
			typeAccumulator[asset.AssetType] = stat
		}
		stat.Count++
		if asset.DerivedStatus == statusExpiring {
			stat.Expiring++
		}
		if asset.DerivedStatus == statusExpired {
			stat.Expired++
		}
	}
	for _, stat := range typeAccumulator {
		stat.MonthlyByCcy = aggregateCosts(costsForType(assets, stat.AssetType))
		result.TypeStats = append(result.TypeStats, *stat)
	}
	sortTypeStats(result.TypeStats)
	if total := convertToBase(result.Costs, settings); total != nil {
		result.TotalMonthly = total
	}

	recent := []Asset{}
	for _, asset := range assets {
		if asset.DaysLeft == nil {
			continue
		}
		recent = append(recent, asset)
	}
	sortByExpireAsc(recent)
	if len(recent) > 10 {
		recent = recent[:10]
	}
	result.RecentExpiring = recent

	response.OK(w, result)
}

func (s *Service) expiring(w http.ResponseWriter, r *http.Request) {
	within := intValue(r.URL.Query().Get("within"), 30)
	if within <= 0 {
		within = 30
	}
	assets, err := s.LoadAssets(r.Context(), assetFilter{
		ExpiringWithin: within,
		Limit:          boundedLimit(r.URL.Query().Get("limit")),
	})
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	sortByExpireAsc(assets)
	response.OK(w, assets)
}

func (s *Service) listCategories(w http.ResponseWriter, r *http.Request) {
	response.OK(w, map[string]interface{}{
		"categories": []map[string]interface{}{
			{"value": categoryPhysical, "label": "实体资产", "types": typesByCategory[categoryPhysical]},
			{"value": categoryVirtual, "label": "虚拟资产", "types": typesByCategory[categoryVirtual]},
		},
		"cost_cycles": []map[string]string{
			{"value": cycleMonthly, "label": "每月"},
			{"value": cycleQuarterly, "label": "每季度"},
			{"value": cycleYearly, "label": "每年"},
			{"value": cycleOneTime, "label": "一次性"},
			{"value": cycleUsage, "label": "按用量"},
		},
	})
}

func (s *Service) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, settings)
}

func (s *Service) updateSettings(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if !decodeJSON(w, r, &payload) {
		return
	}
	settings, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, has := payload["base_currency"]; has {
		settings.BaseCurrency = normalizeCurrency(trimmedString(payload["base_currency"]))
	}
	if _, has := payload["exchange_rates"]; has {
		settings.ExchangeRates = parseFloatMap(jsonString(objectFromPayload(payload["exchange_rates"])))
	}
	if _, has := payload["warn_days"]; has {
		settings.WarnDays = warnDaysFromPayload(payload["warn_days"])
	}
	if err := s.SaveSettings(r.Context(), settings); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	saved, err := s.LoadSettings(r.Context())
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, saved)
}

func (s *Service) resetSettings(w http.ResponseWriter, r *http.Request) {
	defaults := Settings{
		BaseCurrency:  "",
		ExchangeRates: map[string]float64{},
		WarnDays:      append([]int{}, defaultWarnDays...),
	}
	if err := s.SaveSettings(r.Context(), defaults); err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, defaults)
}
