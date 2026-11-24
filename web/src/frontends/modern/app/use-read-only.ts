import { computed, type ComputedRef } from 'vue'

import { useAuthSession } from '@modern/features/auth/auth-session'

// 访问密钥会话只能查看过滤范围内的分组；页面据此隐藏编辑入口并跳过管理员专用请求。
export function useReadOnlySession(): ComputedRef<boolean> {
  const session = useAuthSession()
  return computed(() => session.state.principalType === 'access_key')
}
