package requestlog

import (
	"context"
	"fmt"
	"sort"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

// Each dashboard read is bounded so it costs the same on a busy week as on a
// quiet hour: SQLite has one connection and every other caller waits behind
// it. Latency comes from the most recent successful requests; error causes
// from the most recent failures.
const (
	dashboardLatencySample = 5_000
	dashboardErrorSample   = 20_000
	dashboardRecentErrors  = 8
	dashboardModelLimit    = 8
)

// Failures the dashboard reports. A client cancelling its own request is not a
// gateway failure, and the status index serves this IN list directly.
var dashboardFailureStatuses = []string{"error", "incomplete"}

// DashboardQuery selects a completed-at window, optionally for one access key.
type DashboardQuery struct {
	FromMS      int64
	ToMS        int64
	AccessKeyID *uint
}

// LatencySummary holds percentiles in milliseconds. FirstResponse fields are
// nil when no sampled request recorded a first response time.
type LatencySummary struct {
	Samples          int    `json:"samples"`
	DurationP50MS    int64  `json:"duration_p50_ms"`
	DurationP95MS    int64  `json:"duration_p95_ms"`
	DurationP99MS    int64  `json:"duration_p99_ms"`
	FirstResponseP50 *int64 `json:"first_response_p50_ms"`
	FirstResponseP95 *int64 `json:"first_response_p95_ms"`
}

// ModelLatency is the latency of one client model within the sample.
type ModelLatency struct {
	Model string `json:"model"`
	LatencySummary
}

// ErrorCause counts failed requests sharing a status and error code.
type ErrorCause struct {
	Status    string `json:"status"`
	ErrorCode string `json:"error_code"`
	Count     int64  `json:"count"`
}

// RecentFailure is one failed request, newest first.
type RecentFailure struct {
	RequestID    string `json:"request_id"`
	CompletedAt  int64  `json:"completed_at_ms"`
	Status       string `json:"status"`
	StatusCode   int    `json:"status_code"`
	ErrorCode    string `json:"error_code"`
	ErrorSummary string `json:"error_summary"`
	Model        string `json:"model"`
	GroupID      uint   `json:"group_id"`
	AccessKeyID  uint   `json:"access_key_id"`
	DurationMS   int64  `json:"duration_ms"`
}

// DashboardPerformance is the latency and failure picture of a window.
type DashboardPerformance struct {
	Latency LatencySummary `json:"latency"`
	Models  []ModelLatency `json:"models"`
	// LatencySampleLimit is the most requests the latency sample reads.
	LatencySampleLimit int          `json:"latency_sample_limit"`
	Errors             []ErrorCause `json:"errors"`
	// ErrorsSampled is true when the window had more failures than were read.
	ErrorsSampled bool            `json:"errors_sampled"`
	Recent        []RecentFailure `json:"recent_failures"`
}

type latencyRow struct {
	ClientModel     string
	DurationMs      int64
	FirstResponseMs *int64
}

// QueryDashboardPerformance reads latency percentiles, failure causes and the
// latest failures for a window.
func (service *Service) QueryDashboardPerformance(ctx context.Context, query DashboardQuery) (DashboardPerformance, error) {
	if service == nil || service.db == nil {
		return DashboardPerformance{}, fmt.Errorf("query dashboard: database is nil")
	}
	if query.FromMS < 0 || query.ToMS <= query.FromMS {
		return DashboardPerformance{}, fmt.Errorf("query dashboard: invalid window")
	}
	scope := func() *gorm.DB { return dashboardWindow(service.db.WithContext(ctx), query) }

	var samples []latencyRow
	if err := scope().Where("status = ?", "success").
		Order("completed_at_ms DESC").Limit(dashboardLatencySample).
		Select("client_model, duration_ms, first_response_ms").Scan(&samples).Error; err != nil {
		return DashboardPerformance{}, fmt.Errorf("query dashboard latency: %w", err)
	}

	var failures []failureRow
	if err := scope().Where("status IN ?", dashboardFailureStatuses).
		Order("completed_at_ms DESC").Limit(dashboardErrorSample).
		Select("status, error_code").Scan(&failures).Error; err != nil {
		return DashboardPerformance{}, fmt.Errorf("query dashboard errors: %w", err)
	}

	var recent []models.RequestLog
	if err := scope().Where("status IN ?", dashboardFailureStatuses).
		Order("completed_at_ms DESC").Limit(dashboardRecentErrors).
		Select("id, completed_at_ms, status, status_code, error_code, error_summary, client_model, group_id, access_key_id, duration_ms").
		Find(&recent).Error; err != nil {
		return DashboardPerformance{}, fmt.Errorf("query dashboard recent failures: %w", err)
	}

	result := DashboardPerformance{
		Latency:            summarizeLatency(samples),
		Models:             modelLatencies(samples),
		LatencySampleLimit: dashboardLatencySample,
		Errors:             errorCauses(failures),
		ErrorsSampled:      len(failures) == dashboardErrorSample,
		Recent:             make([]RecentFailure, 0, len(recent)),
	}
	for _, row := range recent {
		result.Recent = append(result.Recent, RecentFailure{
			RequestID: row.ID, CompletedAt: row.CompletedAtMS, Status: row.Status,
			StatusCode: row.StatusCode, ErrorCode: row.ErrorCode, ErrorSummary: row.ErrorSummary,
			Model: row.ClientModel, GroupID: row.GroupID, AccessKeyID: row.AccessKeyID,
			DurationMS: row.DurationMs,
		})
	}
	return result, nil
}

func dashboardWindow(db *gorm.DB, query DashboardQuery) *gorm.DB {
	db = db.Model(&models.RequestLog{}).
		Where("completed_at_ms >= ? AND completed_at_ms < ?", query.FromMS, query.ToMS)
	if query.AccessKeyID != nil {
		db = db.Where("access_key_id = ?", *query.AccessKeyID)
	}
	return db
}

type failureRow struct {
	Status    string
	ErrorCode string
}

func errorCauses(rows []failureRow) []ErrorCause {
	counts := map[[2]string]int64{}
	for _, row := range rows {
		counts[[2]string{row.Status, row.ErrorCode}]++
	}
	causes := make([]ErrorCause, 0, len(counts))
	for key, count := range counts {
		causes = append(causes, ErrorCause{Status: key[0], ErrorCode: key[1], Count: count})
	}
	sort.Slice(causes, func(i, j int) bool {
		if causes[i].Count != causes[j].Count {
			return causes[i].Count > causes[j].Count
		}
		return causes[i].Status+causes[i].ErrorCode < causes[j].Status+causes[j].ErrorCode
	})
	return causes
}

func modelLatencies(samples []latencyRow) []ModelLatency {
	byModel := map[string][]latencyRow{}
	for _, sample := range samples {
		byModel[sample.ClientModel] = append(byModel[sample.ClientModel], sample)
	}
	result := make([]ModelLatency, 0, len(byModel))
	for model, rows := range byModel {
		result = append(result, ModelLatency{Model: model, LatencySummary: summarizeLatency(rows)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Samples != result[j].Samples {
			return result[i].Samples > result[j].Samples
		}
		return result[i].Model < result[j].Model
	})
	if len(result) > dashboardModelLimit {
		result = result[:dashboardModelLimit]
	}
	return result
}

func summarizeLatency(rows []latencyRow) LatencySummary {
	summary := LatencySummary{Samples: len(rows)}
	if len(rows) == 0 {
		return summary
	}
	durations := make([]int64, 0, len(rows))
	firstResponses := make([]int64, 0, len(rows))
	for _, row := range rows {
		durations = append(durations, row.DurationMs)
		if row.FirstResponseMs != nil {
			firstResponses = append(firstResponses, *row.FirstResponseMs)
		}
	}
	summary.DurationP50MS = percentile(durations, 50)
	summary.DurationP95MS = percentile(durations, 95)
	summary.DurationP99MS = percentile(durations, 99)
	if len(firstResponses) > 0 {
		p50, p95 := percentile(firstResponses, 50), percentile(firstResponses, 95)
		summary.FirstResponseP50, summary.FirstResponseP95 = &p50, &p95
	}
	return summary
}

// percentile uses the nearest-rank method on a copy of the values.
func percentile(values []int64, rank int) int64 {
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := (rank*len(sorted)+99)/100 - 1
	return sorted[min(max(index, 0), len(sorted)-1)]
}
