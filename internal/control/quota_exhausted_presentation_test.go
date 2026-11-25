package control

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"gpt-load/internal/platform/config"
	"gpt-load/internal/state"
	"gpt-load/internal/storage/models"

	"github.com/gin-gonic/gin"
)

// 账号级额度耗尽的凭据在管理面按冷却到重置展示：全部耗尽的分组是「不可用」而不是「受限」。
func TestQuotaExhaustedCredentialsPresentAsCooldownUntilReset(t *testing.T) {
	t.Parallel()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	exhausted := createGroupCollectionGroup(t, fixture, "exhausted", true, nil)
	partial := createGroupCollectionGroup(t, fixture, "partial", true, nil)
	entries := []state.CredentialEntry{
		createGroupCollectionKey(t, fixture, exhausted.ID, models.CredentialStatusActive, nil),
		createGroupCollectionKey(t, fixture, exhausted.ID, models.CredentialStatusActive, nil),
		createGroupCollectionKey(t, fixture, partial.ID, models.CredentialStatusActive, nil),
		createGroupCollectionKey(t, fixture, partial.ID, models.CredentialStatusActive, nil),
	}
	publishGroupCollectionRuntime(t, fixture, entries)
	resetAt := time.Now().Add(72 * time.Hour).Truncate(time.Millisecond).UTC()
	zero, plenty := 0.0, 0.8
	for _, quota := range []struct {
		id        uint
		remaining *float64
	}{{entries[0].ID, &zero}, {entries[1].ID, &zero}, {entries[2].ID, &zero}, {entries[3].ID, &plenty}} {
		if !fixture.registry.SetCredentialQuotaObservation(quota.id, quota.remaining, resetAt) {
			t.Fatalf("SetCredentialQuotaObservation(%d) = false", quota.id)
		}
	}

	engine := gin.New()
	NewServer(&config.Config{AuthKey: authTestKey}, fixture.service).RegisterRoutes(engine)
	result := performGroupCollectionRequest(engine, "/api/modern/groups", "Bearer "+authTestKey)
	if result.Code != http.StatusOK {
		t.Fatalf("workspace status = %d: %s", result.Code, result.Body.String())
	}
	var data struct {
		Items []struct {
			ID           uint   `json:"id"`
			Availability string `json:"availability"`
		} `json:"items"`
	}
	if err := json.Unmarshal(decodeGroupCollectionSuccessData(t, result), &data); err != nil {
		t.Fatal(err)
	}
	availability := map[uint]string{}
	for _, item := range data.Items {
		availability[item.ID] = item.Availability
	}
	if availability[exhausted.ID] != "unavailable" || availability[partial.ID] != "limited" {
		t.Fatalf("availability = %v, want exhausted unavailable and partial limited", availability)
	}

	credentials, err := fixture.service.ListGroupCredentials(t.Context(), exhausted.ID, CredentialCollectionQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListGroupCredentials() error = %v", err)
	}
	if credentials.Summary.Available != 0 || credentials.Summary.Cooldown != 2 {
		t.Fatalf("summary = %#v, want 0 available and 2 cooldown", credentials.Summary)
	}
	for _, item := range credentials.Items {
		if item.EffectiveStatus != string(healthBucketCooldown) || item.CooldownUntilMS == nil ||
			*item.CooldownUntilMS != resetAt.UnixMilli() || item.Recovery.Mode != "cooldown" {
			t.Fatalf("credential %d = status %q until %v recovery %#v, want cooldown until quota reset",
				item.CredentialID, item.EffectiveStatus, item.CooldownUntilMS, item.Recovery)
		}
	}
}
