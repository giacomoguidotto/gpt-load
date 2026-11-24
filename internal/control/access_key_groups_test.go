package control

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/platform/config"
	"gpt-load/internal/storage/models"
)

type accessKeyGroupsFixture struct {
	engine          *gin.Engine
	allowedGroup    uint
	allowedCred     uint
	hiddenGroup     uint
	hiddenCred      uint
	apiKeyGroup     uint
	scopedKey       string
	unrestrictedKey string
}

const accessKeyGroupsAPISecret = "sk-access-key-groups-secret-value"

func newAccessKeyGroupsFixture(t *testing.T) accessKeyGroupsFixture {
	t.Helper()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	now := time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return now }
	allowedGroup, allowedCred := createHomeSubscriptionCredential(t, fixture, "allowed", "allowed-account", "allowed-dev@example.com")
	hiddenGroup, hiddenCred := createHomeSubscriptionCredential(t, fixture, "hidden", "hidden-account", "hidden-dev@example.com")
	apiKeyGroup := createGroupWithCredentials(t, fixture, accessKeyGroupsAPISecret)
	createAccessKeyGroupsObservation(t, fixture, allowedCred, now)
	scoped, err := fixture.service.CreateAccessKey(t.Context(), AccessKeyCreateRequest{
		Name:    "scoped viewer",
		Filters: &AccessKeyFilters{Groups: []uint{allowedGroup, apiKeyGroup}},
	})
	if err != nil {
		t.Fatal(err)
	}
	unrestricted, err := fixture.service.CreateAccessKey(t.Context(), AccessKeyCreateRequest{Name: "unrestricted viewer"})
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	NewServer(&config.Config{AuthKey: authTestKey}, fixture.service).RegisterRoutes(engine)
	return accessKeyGroupsFixture{
		engine: engine, allowedGroup: allowedGroup, allowedCred: allowedCred.ID,
		hiddenGroup: hiddenGroup, hiddenCred: hiddenCred.ID, apiKeyGroup: apiKeyGroup,
		scopedKey: scoped.Key, unrestrictedKey: unrestricted.Key,
	}
}

func createAccessKeyGroupsObservation(t *testing.T, fixture serviceFixture, credential models.Credential, observedAt time.Time) {
	t.Helper()
	remaining := 0.5
	resetAtMS := observedAt.Add(5 * time.Hour).UnixMilli()
	snapshot, err := json.Marshal(CredentialObservationSnapshot{
		Plan:    ObservationPlanSummary{Name: "pro", Level: "premium"},
		Account: &ObservationAccountSummary{DisplayName: "Allowed Person", Email: "allowed-dev@example.com", OrganizationName: "allowed-dev@example.com's Organization"},
		QuotaWindows: []ObservationQuotaWindow{{
			ID: "five-hour", Label: "5h", Scope: "account", Unit: "ratio",
			Remaining: &remaining, ResetAtMS: &resetAtMS, State: "available",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	observedAtMS := observedAt.UnixMilli()
	row := models.CredentialObservation{
		CredentialID: credential.ID, IdentityFingerprint: credential.IdentityFingerprint,
		SchemaVersion: 1, ObservationVersion: 1, SnapshotJSON: snapshot,
		State: models.CredentialObservationFresh, ObservedAtMS: &observedAtMS,
		LastAttemptAtMS: &observedAtMS, UpdatedAtMS: observedAtMS,
	}
	if err := fixture.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func accessKeyGroupsRequest(engine *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader("{}"))
	request.Header.Set("Accept-Language", "en-US")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func modernGroupIDs(t *testing.T, recorder *httptest.ResponseRecorder) []uint {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("groups status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			Items []struct {
				ID       uint   `json:"id"`
				Endpoint string `json:"endpoint"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	ids := make([]uint, 0, len(envelope.Data.Items))
	for _, item := range envelope.Data.Items {
		ids = append(ids, item.ID)
	}
	slices.Sort(ids)
	return ids
}

func TestAccessKeyGroupListIsLimitedToFilteredGroups(t *testing.T) {
	t.Parallel()
	f := newAccessKeyGroupsFixture(t)
	want := []uint{f.allowedGroup, f.apiKeyGroup}
	slices.Sort(want)
	if got := modernGroupIDs(t, accessKeyGroupsRequest(f.engine, http.MethodGet, "/api/modern/groups", f.scopedKey)); !slices.Equal(got, want) {
		t.Fatalf("scoped access key groups = %v, want %v", got, want)
	}
	all := []uint{f.allowedGroup, f.hiddenGroup, f.apiKeyGroup}
	slices.Sort(all)
	if got := modernGroupIDs(t, accessKeyGroupsRequest(f.engine, http.MethodGet, "/api/modern/groups", f.unrestrictedKey)); !slices.Equal(got, all) {
		t.Fatalf("unrestricted access key groups = %v, want %v", got, all)
	}
	if got := modernGroupIDs(t, accessKeyGroupsRequest(f.engine, http.MethodGet, "/api/modern/groups", authTestKey)); !slices.Equal(got, all) {
		t.Fatalf("admin groups = %v, want %v", got, all)
	}
}

func TestAccessKeyOutOfScopeGroupReadsReturnNotFound(t *testing.T) {
	t.Parallel()
	f := newAccessKeyGroupsFixture(t)
	for _, path := range []string{
		fmt.Sprintf("/api/groups/%d", f.hiddenGroup),
		fmt.Sprintf("/api/modern/groups/%d/credentials", f.hiddenGroup),
		fmt.Sprintf("/api/modern/groups/%d/credentials/%d", f.hiddenGroup, f.hiddenCred),
	} {
		if recorder := accessKeyGroupsRequest(f.engine, http.MethodGet, path, f.scopedKey); recorder.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, recorder.Code)
		}
		if recorder := accessKeyGroupsRequest(f.engine, http.MethodGet, path, authTestKey); recorder.Code != http.StatusOK {
			t.Fatalf("admin GET %s status = %d, want 200", path, recorder.Code)
		}
	}
	for _, path := range []string{
		fmt.Sprintf("/api/groups/%d", f.allowedGroup),
		fmt.Sprintf("/api/modern/groups/%d/credentials", f.allowedGroup),
		fmt.Sprintf("/api/modern/groups/%d/credentials/%d", f.allowedGroup, f.allowedCred),
		"/api/channels",
	} {
		if recorder := accessKeyGroupsRequest(f.engine, http.MethodGet, path, f.scopedKey); recorder.Code != http.StatusOK {
			t.Fatalf("scoped GET %s status = %d, want 200", path, recorder.Code)
		}
	}
}

// collectJSONKeys returns every object key found anywhere in a JSON document.
func collectJSONKeys(value any, keys map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			keys[key] = struct{}{}
			collectJSONKeys(child, keys)
		}
	case []any:
		for _, child := range typed {
			collectJSONKeys(child, keys)
		}
	}
}

func TestAccessKeyCredentialResponsesContainNoSecretFields(t *testing.T) {
	t.Parallel()
	f := newAccessKeyGroupsFixture(t)
	forbiddenKeys := []string{
		"email", "display_url", "proxy_id", "proxy_name", "reference_state", "last_failure_summary",
		"params", "token", "access_token", "refresh_token", "api_key", "credentials_json",
		"auth_file", "headers", "header_overrides", "base_url", "secret", "value",
	}
	paths := []string{
		fmt.Sprintf("/api/modern/groups/%d/credentials", f.allowedGroup),
		fmt.Sprintf("/api/modern/groups/%d/credentials/%d", f.allowedGroup, f.allowedCred),
		fmt.Sprintf("/api/modern/groups/%d/credentials", f.apiKeyGroup),
	}
	for _, path := range paths {
		recorder := accessKeyGroupsRequest(f.engine, http.MethodGet, path, f.scopedKey)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", path, recorder.Code, recorder.Body.String())
		}
		body := recorder.Body.String()
		var decoded any
		if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
			t.Fatal(err)
		}
		keys := map[string]struct{}{}
		collectJSONKeys(decoded, keys)
		for _, key := range forbiddenKeys {
			if _, exists := keys[key]; exists {
				t.Fatalf("GET %s exposed field %q to an access key", path, key)
			}
		}
		for _, secret := range []string{accessKeyGroupsAPISecret, "allowed-dev@example.com", "Allowed Person"} {
			if strings.Contains(body, secret) {
				t.Fatalf("GET %s exposed %q to an access key", path, secret)
			}
		}
		for _, key := range []string{"mask", "priority", "weight", "effective_status", "cooldown_until_ms"} {
			if _, exists := keys[key]; !exists {
				t.Fatalf("GET %s omitted read-only field %q", path, key)
			}
		}
	}
	detail := accessKeyGroupsRequest(f.engine, http.MethodGet,
		fmt.Sprintf("/api/modern/groups/%d/credentials/%d", f.allowedGroup, f.allowedCred), f.scopedKey)
	if !strings.Contains(detail.Body.String(), `"quota_windows"`) || !strings.Contains(detail.Body.String(), `"email_mask"`) {
		t.Fatal("access key detail must keep quota windows and the masked account")
	}
	summary := accessKeyGroupsRequest(f.engine, http.MethodGet, fmt.Sprintf("/api/groups/%d", f.allowedGroup), f.scopedKey)
	if !strings.Contains(summary.Body.String(), `"params":{}`) {
		t.Fatalf("group summary params must be empty for access keys: %s", summary.Body.String())
	}
	admin := accessKeyGroupsRequest(f.engine, http.MethodGet,
		fmt.Sprintf("/api/modern/groups/%d/credentials/%d", f.allowedGroup, f.allowedCred), authTestKey)
	if !strings.Contains(admin.Body.String(), `"email":"allowed-dev@example.com"`) {
		t.Fatal("admin credential detail must remain unredacted")
	}
}

func TestAccessKeyCannotMutateOrReadAdminGroupRoutes(t *testing.T) {
	t.Parallel()
	f := newAccessKeyGroupsFixture(t)
	group := f.allowedGroup
	credential := f.allowedCred
	for _, test := range []struct{ method, path string }{
		{http.MethodPost, "/api/groups"},
		{http.MethodPut, fmt.Sprintf("/api/groups/%d/settings", group)},
		{http.MethodPut, fmt.Sprintf("/api/groups/%d/models", group)},
		{http.MethodDelete, fmt.Sprintf("/api/groups/%d", group)},
		{http.MethodPut, fmt.Sprintf("/api/groups/%d/credentials/%d", group, credential)},
		{http.MethodDelete, fmt.Sprintf("/api/groups/%d/credentials/%d", group, credential)},
		{http.MethodPost, fmt.Sprintf("/api/groups/%d/credentials/%d/reveal", group, credential)},
		{http.MethodPost, fmt.Sprintf("/api/groups/%d/credentials/%d/download", group, credential)},
		{http.MethodPost, fmt.Sprintf("/api/groups/%d/credentials/download-all", group)},
		{http.MethodPost, fmt.Sprintf("/api/groups/%d/credentials/batch", group)},
		{http.MethodGet, fmt.Sprintf("/api/groups/%d/settings", group)},
		{http.MethodGet, fmt.Sprintf("/api/groups/%d/credentials", group)},
		{http.MethodGet, fmt.Sprintf("/api/groups/%d/credentials/%d", group, credential)},
		{http.MethodGet, "/api/groups"},
		{http.MethodGet, "/api/settings"},
		{http.MethodGet, "/api/access-keys"},
		{http.MethodGet, "/api/proxies"},
		{http.MethodGet, "/api/modern/credentials/options"},
		{http.MethodGet, "/api/credential-stages/stage-1"},
	} {
		for _, token := range []string{f.scopedKey, f.unrestrictedKey} {
			if recorder := accessKeyGroupsRequest(f.engine, test.method, test.path, token); recorder.Code != http.StatusForbidden {
				t.Fatalf("%s %s status = %d, want 403", test.method, test.path, recorder.Code)
			}
		}
	}
	if recorder := accessKeyGroupsRequest(f.engine, http.MethodGet, fmt.Sprintf("/api/groups/%d/settings", group), authTestKey); recorder.Code != http.StatusOK {
		t.Fatalf("admin group settings status = %d, want 200", recorder.Code)
	}
}
