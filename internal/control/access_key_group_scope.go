package control

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"gpt-load/internal/outboundproxy"
	app_errors "gpt-load/internal/platform/errors"
	"gpt-load/internal/state"
)

// accessKeyGroupScope 描述访问密钥可只读查看的分组。groups 为 nil 表示该密钥可路由到全部分组，
// 因此也可查看全部分组；非空时只允许过滤列表中的分组。
type accessKeyGroupScope struct {
	groups map[uint]struct{}
}

func (scope *accessKeyGroupScope) allows(groupID uint) bool {
	if scope == nil || scope.groups == nil {
		return true
	}
	_, ok := scope.groups[groupID]
	return ok
}

// currentAccessKeyGroupScope 对管理员返回 nil；对访问密钥返回其分组范围，调用方据此过滤并脱敏。
func (s *Server) currentAccessKeyGroupScope(c *gin.Context) (*accessKeyGroupScope, error) {
	accessKeyID, scoped := currentAccessKeyID(c)
	if !scoped {
		return nil, nil
	}
	if s == nil || s.service == nil || s.service.manager == nil {
		return nil, app_errors.ErrUnauthorized
	}
	snapshot := s.service.manager.Current()
	if snapshot == nil {
		return nil, app_errors.ErrUnauthorized
	}
	accessKey, exists := snapshot.AccessKeysByID[accessKeyID]
	if !exists || accessKey.Status != state.AccessKeyStatusActive {
		return nil, app_errors.ErrUnauthorized
	}
	scope := &accessKeyGroupScope{}
	if len(accessKey.Filters.Groups) > 0 {
		scope.groups = make(map[uint]struct{}, len(accessKey.Filters.Groups))
		for id := range accessKey.Filters.Groups {
			scope.groups[id] = struct{}{}
		}
	}
	return scope, nil
}

// authorizeAccessKeyGroup 让范围外的分组与不存在的分组返回同样的 404，避免枚举。
func (s *Server) authorizeAccessKeyGroup(
	c *gin.Context,
	operation string,
	groupID uint,
) (*accessKeyGroupScope, bool) {
	scope, err := s.currentAccessKeyGroupScope(c)
	if err != nil {
		writeServiceError(c, operation, err)
		return nil, false
	}
	if !scope.allows(groupID) {
		writeServiceError(c, operation, app_errors.ErrResourceNotFound)
		return nil, false
	}
	return scope, true
}

// 访问密钥只读视图不返回账号邮箱原文、上游失败原文与代理地址；掩码与状态保持不变。
func redactCredentialItemForAccessKey(item CredentialItemResponse) CredentialItemResponse {
	item.Account.Email = ""
	item.LastFailureSummary = ""
	item.Proxy = outboundproxy.View{
		ConfiguredMode:  item.Proxy.ConfiguredMode,
		EffectiveMode:   item.Proxy.EffectiveMode,
		EffectiveSource: item.Proxy.EffectiveSource,
	}
	if item.Observation != nil {
		observation := redactObservationForAccessKey(*item.Observation)
		item.Observation = &observation
	}
	return item
}

func redactObservationForAccessKey(observation CredentialObservationResponse) CredentialObservationResponse {
	if observation.Snapshot == nil || observation.Snapshot.Account == nil {
		return observation
	}
	snapshot := *observation.Snapshot
	account := *snapshot.Account
	account.Email = ""
	// Claude 默认组织名形如 "<email>'s Organization"，显示名也可能是真实姓名，一并清空。
	account.DisplayName = ""
	account.OrganizationName = ""
	snapshot.Account = &account
	observation.Snapshot = &snapshot
	return observation
}

// 分组参数可能包含自定义上游地址或附加参数，访问密钥只看到空对象。
func redactGroupSummaryForAccessKey(summary GroupSummaryResponse) GroupSummaryResponse {
	summary.Params = json.RawMessage(`{}`)
	return summary
}

func redactModernGroupForAccessKey(item ModernGroupItem) ModernGroupItem {
	item.Endpoint = ""
	return item
}
