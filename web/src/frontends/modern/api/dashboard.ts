import type { ApiClient } from '@shared/http/client'
import { boolean, integer, list, record, text } from './response'

export interface LatencySummary {
  samples: number
  p50: number
  p95: number
  p99: number
  firstResponseP50: number | null
  firstResponseP95: number | null
}
export interface ModelLatency extends LatencySummary {
  model: string
}
export interface ErrorCause {
  status: string
  errorCode: string
  count: number
}
export interface RecentFailure {
  requestId: string
  completedAt: number
  status: string
  statusCode: number
  errorCode: string
  errorSummary: string
  model: string
  groupId: number
  accessKeyId: number
  durationMs: number
}
export interface Dashboard {
  from: number
  to: number
  observedAt: number
  latency: LatencySummary
  models: ModelLatency[]
  latencySampleLimit: number
  errors: ErrorCause[]
  errorsSampled: boolean
  recent: RecentFailure[]
}

const optional = (value: unknown) => (value == null ? null : integer(value))

function readLatency(value: unknown): LatencySummary {
  const row = record(value)
  return {
    samples: integer(row.samples),
    p50: integer(row.duration_p50_ms),
    p95: integer(row.duration_p95_ms),
    p99: integer(row.duration_p99_ms),
    firstResponseP50: optional(row.first_response_p50_ms),
    firstResponseP95: optional(row.first_response_p95_ms),
  }
}

export async function getDashboard(
  client: ApiClient,
  window: { from: number; to: number },
  signal: AbortSignal,
): Promise<Dashboard> {
  const params = new URLSearchParams({ from_ms: String(window.from), to_ms: String(window.to) })
  const data = record(await client.request(`/api/modern/dashboard?${params}`, { signal }))
  return {
    from: integer(data.from_ms),
    to: integer(data.to_ms),
    observedAt: integer(data.observed_at_ms),
    latency: readLatency(data.latency),
    models: list(data.models).map((value) => ({
      model: text(record(value).model),
      ...readLatency(value),
    })),
    latencySampleLimit: integer(data.latency_sample_limit),
    errors: list(data.errors).map((value) => {
      const row = record(value)
      return {
        status: text(row.status),
        errorCode: text(row.error_code),
        count: integer(row.count),
      }
    }),
    errorsSampled: boolean(data.errors_sampled),
    recent: list(data.recent_failures).map((value) => {
      const row = record(value)
      return {
        requestId: text(row.request_id),
        completedAt: integer(row.completed_at_ms),
        status: text(row.status),
        statusCode: integer(row.status_code),
        errorCode: text(row.error_code),
        errorSummary: text(row.error_summary),
        model: text(row.model),
        groupId: integer(row.group_id),
        accessKeyId: integer(row.access_key_id),
        durationMs: integer(row.duration_ms),
      }
    }),
  }
}
