package control

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"

	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/platform/response"
	stateloader "gpt-load/internal/state/loader"
	"gpt-load/internal/storage/models"
	"gpt-load/internal/subscription"
)

// capacityPageSize is the largest credential page the collection mapper
// accepts; larger pools are read page by page.
const capacityPageSize = 100

// CapacityAccountResponse is one credential with forecasts for its quota
// windows.
type CapacityAccountResponse struct {
	GroupID    uint                   `json:"group_id"`
	Credential CredentialItemResponse `json:"credential"`
	Forecasts  []QuotaForecast        `json:"forecasts"`
}

// CapacityResponse lists every credential of every pool with how long its
// quota lasts at the current pace.
type CapacityResponse struct {
	ObservedAtMS int64                     `json:"observed_at_ms"`
	Accounts     []CapacityAccountResponse `json:"accounts"`
}

func (s *Server) handleCapacity(c *gin.Context) {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		writeServiceError(c, "capacity", app_errors.ErrBadRequest)
		return
	}
	scope, err := s.currentAccessKeyGroupScope(c)
	if err != nil {
		writeServiceError(c, "capacity", err)
		return
	}
	result, err := s.service.Capacity(c.Request.Context(), func(groupID uint) bool {
		return scope == nil || scope.allows(groupID)
	})
	if err != nil {
		writeServiceError(c, "capacity", err)
		return
	}
	if scope != nil {
		for index := range result.Accounts {
			result.Accounts[index].Credential = redactCredentialItemForAccessKey(result.Accounts[index].Credential)
		}
	}
	c.Header("Cache-Control", "no-store")
	response.SuccessI18n(c, "common.success", result)
}

// Capacity reads each visible pool's credentials from the same snapshot the
// credential list uses, without per-credential activity queries, and
// forecasts every percent quota window from the last few hours of history.
func (s *Service) Capacity(ctx context.Context, visible func(groupID uint) bool) (CapacityResponse, error) {
	var groups []models.Group
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&groups).Error; err != nil {
		return CapacityResponse{}, app_errors.ParseDBError(err)
	}
	now := s.now().UTC()
	nowMS, err := safeEpochMilliseconds(now)
	if err != nil {
		return CapacityResponse{}, err
	}
	result := CapacityResponse{ObservedAtMS: nowMS, Accounts: []CapacityAccountResponse{}}
	for _, group := range groups {
		if !visible(group.ID) {
			continue
		}
		accounts, err := s.capacityAccounts(ctx, group.ID, nowMS)
		if err != nil {
			return CapacityResponse{}, err
		}
		result.Accounts = append(result.Accounts, accounts...)
	}
	return result, nil
}

func (s *Service) capacityAccounts(ctx context.Context, groupID uint, nowMS int64) ([]CapacityAccountResponse, error) {
	capture, err := s.captureCredentials(ctx, groupID)
	if err != nil {
		return nil, err
	}
	observation, err := validateCredentialCapture(capture)
	if err != nil {
		return nil, err
	}
	history, err := s.recentQuotaHistory(ctx, capture, nowMS)
	if err != nil {
		return nil, err
	}

	accounts := make([]CapacityAccountResponse, 0, len(capture.rows))
	for page := 1; ; page++ {
		collection, err := s.mapCredentialCollection(ctx, observation, CredentialCollectionQuery{Page: page, PageSize: capacityPageSize})
		if err != nil {
			return nil, err
		}
		for _, item := range collection.Items {
			accounts = append(accounts, CapacityAccountResponse{
				GroupID:    groupID,
				Credential: item,
				Forecasts:  forecastCredentialQuota(item, history[item.CredentialID], nowMS),
			})
		}
		if len(collection.Items) < capacityPageSize {
			return accounts, nil
		}
	}
}

// recentQuotaHistory returns, per credential and window id, the samples
// recorded for the credential's current identity within the pace lookback.
func (s *Service) recentQuotaHistory(
	ctx context.Context,
	capture credentialCapture,
	nowMS int64,
) (map[uint]map[string][]quotaHistorySample, error) {
	history := make(map[uint]map[string][]quotaHistorySample)
	if normalizeGroupConnectionType(capture.group.ConnectionType) != models.ConnectionTypeSubscription || len(capture.rows) == 0 {
		return history, nil
	}
	identities := make(map[uint]string, len(capture.rows))
	for _, credential := range capture.rows {
		identities[credential.ID] = subscription.QuotaHistoryTargetIdentity(stateloader.CredentialIdentityGeneration(
			credential.IdentityFingerprint, capture.group.ChannelID,
			string(capture.group.ConnectionType), json.RawMessage(capture.group.Params)))
	}
	var rows []models.CredentialQuotaHistory
	if err := s.db.WithContext(ctx).
		Where("group_id = ? AND credential_id IN ? AND observed_at_ms >= ?",
			capture.group.ID, credentialIDs(capture.rows), nowMS-quotaRecentPaceLookbackMS).
		Find(&rows).Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}
	for _, row := range rows {
		if row.TargetIdentity != identities[row.CredentialID] {
			continue
		}
		windows := history[row.CredentialID]
		if windows == nil {
			windows = make(map[string][]quotaHistorySample)
			history[row.CredentialID] = windows
		}
		windows[row.WindowID] = append(windows[row.WindowID], quotaHistorySample{
			ObservedAtMS: row.ObservedAtMS, UsedBasisPoints: row.UsedBasisPoints, ResetAtMS: row.ResetAtMS,
		})
	}
	return history, nil
}

func forecastCredentialQuota(
	item CredentialItemResponse,
	history map[string][]quotaHistorySample,
	nowMS int64,
) []QuotaForecast {
	forecasts := []QuotaForecast{}
	if item.Observation == nil || item.Observation.Snapshot == nil {
		return forecasts
	}
	observedAt := nowMS
	if item.Observation.ObservedAtMS != nil {
		observedAt = *item.Observation.ObservedAtMS
	}
	for _, window := range item.Observation.Snapshot.QuotaWindows {
		used, ok := quotaWindowUsedBasisPoints(window)
		if !ok {
			continue
		}
		forecasts = append(forecasts, forecastQuotaWindow(quotaForecastInput{
			WindowID:        window.ID,
			UsedBasisPoints: used,
			NowMS:           nowMS,
			ObservedAtMS:    observedAt,
			ResetAtMS:       window.ResetAtMS,
			WindowSeconds:   window.WindowSeconds,
			History:         history[window.ID],
		}))
	}
	return forecasts
}
