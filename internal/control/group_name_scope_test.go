package control

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/channel"
	"gpt-load/internal/platform/config"
	app_errors "gpt-load/internal/platform/errors"
)

// createNamedChannelGroup 在指定渠道创建一个带显式名称的 API key 分组。
func createNamedChannelGroup(
	fixture serviceFixture,
	t *testing.T,
	name string,
	channelID channel.ID,
	params string,
	credential string,
) (GroupCreateResult, error) {
	t.Helper()
	return fixture.service.CreateGroup(t.Context(), GroupCreateRequest{
		Name: &name, ChannelID: channelID, Params: json.RawMessage(params),
		Models:      optionalGroupModels{Set: true, Values: []GroupModel{{ID: "gpt-4o"}}},
		Credentials: credential, ConnectionType: "api_key",
	})
}

func assertDuplicateGroupName(t *testing.T, err error) {
	t.Helper()
	var apiErr *app_errors.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != app_errors.ErrDuplicateResource.Code ||
		apiErr.HTTPStatus != http.StatusConflict {
		t.Fatalf("error = %v, want %v (409)", err, app_errors.ErrDuplicateResource)
	}
}

func TestCreateGroupAllowsTheSameNameOnDifferentChannels(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	first, err := createNamedChannelGroup(fixture, t, "shared", channel.OpenAI, `{}`, "sk-scope-openai")
	if err != nil {
		t.Fatalf("CreateGroup(openai) error = %v", err)
	}
	second, err := createNamedChannelGroup(
		fixture, t, "shared", channel.OpenAICompatible,
		`{"base_url":"https://relay.example/v1"}`, "sk-scope-compatible",
	)
	if err != nil {
		t.Fatalf("CreateGroup(openai-compatible) error = %v", err)
	}
	if first.GroupID == second.GroupID || first.GroupName != "shared" || second.GroupName != "shared" {
		t.Fatalf("created groups = %#v %#v", first, second)
	}
}

func TestCreateGroupRejectsTheSameNameOnTheSameChannel(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	if _, err := createNamedChannelGroup(
		fixture, t, "shared", channel.OpenAICompatible,
		`{"base_url":"https://first.example/v1"}`, "sk-same-first",
	); err != nil {
		t.Fatalf("CreateGroup(first) error = %v", err)
	}
	_, err := createNamedChannelGroup(
		fixture, t, "shared", channel.OpenAICompatible,
		`{"base_url":"https://second.example/v1"}`, "sk-same-second",
	)
	assertDuplicateGroupName(t, err)
}

func TestUpdateGroupSettingsRenameRespectsChannelScope(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	if _, err := createNamedChannelGroup(fixture, t, "taken", channel.OpenAI, `{}`, "sk-rename-openai"); err != nil {
		t.Fatal(err)
	}
	if _, err := createNamedChannelGroup(
		fixture, t, "sibling", channel.OpenAICompatible,
		`{"base_url":"https://sibling.example/v1"}`, "sk-rename-sibling",
	); err != nil {
		t.Fatal(err)
	}
	target, err := createNamedChannelGroup(
		fixture, t, "target", channel.OpenAICompatible,
		`{"base_url":"https://target.example/v1"}`, "sk-rename-target",
	)
	if err != nil {
		t.Fatal(err)
	}

	// 其他渠道占用的名称可以使用。
	if _, err := fixture.service.UpdateGroupSettings(t.Context(), target.GroupID, GroupSettingsUpdateRequest{
		Name: optionalField[string]{Set: true, Value: "taken"},
	}); err != nil {
		t.Fatalf("rename to a name used on another channel error = %v", err)
	}
	// 同渠道已占用的名称被拒绝，且原名称保留。
	_, err = fixture.service.UpdateGroupSettings(t.Context(), target.GroupID, GroupSettingsUpdateRequest{
		Name: optionalField[string]{Set: true, Value: "sibling"},
	})
	assertDuplicateGroupName(t, err)
	if stored := mustLoadGroup(t, fixture, target.GroupID); stored.Name != "taken" {
		t.Fatalf("stored name = %q, want taken", stored.Name)
	}
}

func TestUpdateGroupChannelRejectsANameTakenOnTheTargetChannel(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	existing, err := createNamedChannelGroup(fixture, t, "jack", channel.OpenAI, `{}`, "sk-switch-existing")
	if err != nil {
		t.Fatal(err)
	}
	moving, err := createNamedChannelGroup(
		fixture, t, "jack", channel.OpenAICompatible,
		`{"base_url":"https://moving.example/v1"}`, "sk-switch-moving",
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = fixture.service.UpdateGroupChannel(t.Context(), moving.GroupID, GroupChannelUpdateRequest{
		ChannelID: channel.OpenAI,
	})
	assertDuplicateGroupName(t, err)
	if stored := mustLoadGroup(t, fixture, moving.GroupID); stored.ChannelID != string(channel.OpenAICompatible) {
		t.Fatalf("stored channel = %q, want unchanged", stored.ChannelID)
	}
	if stored := mustLoadGroup(t, fixture, existing.GroupID); stored.ChannelID != string(channel.OpenAI) {
		t.Fatalf("existing group changed: %q", stored.ChannelID)
	}
}

func TestGroupChannelHTTPReportsNameConflictOnTheTargetChannel(t *testing.T) {
	t.Parallel()
	initControlI18n(t)
	fixture := newServiceFixture(t)
	if _, err := createNamedChannelGroup(fixture, t, "jack", channel.OpenAI, `{}`, "sk-http-existing"); err != nil {
		t.Fatal(err)
	}
	moving, err := createNamedChannelGroup(
		fixture, t, "jack", channel.OpenAICompatible,
		`{"base_url":"https://http-moving.example/v1"}`, "sk-http-moving",
	)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	NewServer(&config.Config{AuthKey: "test-auth-key"}, fixture.service).RegisterRoutes(engine)

	recorder := serveGroupSettingsRequest(
		t, engine, http.MethodPut, "/api/groups/"+stringGroupID(moving.GroupID)+"/channel",
		"test-auth-key", `{"channel_id":"openai"}`,
	)
	assertUpdateGroupErrorResponse(
		t, recorder, http.StatusConflict, app_errors.ErrDuplicateResource.Code, "分组名称已存在",
	)
}

func TestCreateGroupGeneratesNamesPerChannel(t *testing.T) {
	t.Parallel()
	fixture := newServiceFixture(t)
	create := func(channelID channel.ID, credential string) string {
		t.Helper()
		result, err := fixture.service.CreateGroup(t.Context(), GroupCreateRequest{
			ChannelID: channelID, Params: json.RawMessage(`{"base_url":"https://same-host.example/v1"}`),
			Models:      optionalGroupModels{Set: true, Values: []GroupModel{{ID: "gpt-4o"}}},
			Credentials: credential, ConnectionType: "api_key", ConfirmSameTarget: true,
		})
		if err != nil {
			t.Fatalf("CreateGroup(%q) error = %v", channelID, err)
		}
		return result.GroupName
	}
	first := create(channel.OpenAICompatible, "sk-generated-first")
	second := create(channel.OpenAICompatible, "sk-generated-second")
	other := create(channel.OpenAI, "sk-generated-other")
	if first == second {
		t.Fatalf("same-channel generated names collide: %q", first)
	}
	if other != first {
		t.Fatalf("other channel generated %q, want the unsuffixed %q", other, first)
	}
}
