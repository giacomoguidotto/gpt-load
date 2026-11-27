package requestlog

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"gpt-load/internal/platform/epochms"
	"gpt-load/internal/storage/models"
)

type RetentionPolicyProvider interface {
	RequestLogRetentionDays() int
}

const (
	retentionBatchSize                   = 1000
	usageAggregationJournalRetentionDays = 35
	quotaHistoryRetentionDays            = 35
)

// Sweep removes request logs and aggregation journals strictly older than
// their respective retention boundaries. Hourly aggregates are retained
// indefinitely. Failures are isolated from the data plane.
func (service *Service) Sweep(ctx context.Context, now time.Time) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return
	}

	days := service.retentionPolicy.RequestLogRetentionDays()
	nowMS, err := epochms.FromTime(now)
	if err != nil {
		service.recordRetentionDeleteFailure(now)
		return
	}
	requestLogCutoffMS, err := retentionCutoffMS(nowMS, days)
	if err != nil {
		service.recordRetentionDeleteFailure(now)
		return
	}
	journalCutoffMS, err := retentionCutoffMS(nowMS, usageAggregationJournalRetentionDays)
	if err != nil {
		service.recordRetentionDeleteFailure(now)
		return
	}
	quotaHistoryCutoffMS, err := retentionCutoffMS(nowMS, quotaHistoryRetentionDays)
	if err != nil {
		service.recordRetentionDeleteFailure(now)
		return
	}
	journalCutoffMS, err = epochms.AlignDown(
		journalCutoffMS,
		epochms.MillisecondsPerHour,
	)
	if err != nil {
		service.recordRetentionDeleteFailure(now)
		return
	}

	service.deleteExpiredRequestLogs(ctx, requestLogCutoffMS, now)
	if ctx.Err() != nil {
		return
	}
	service.deleteExpiredUsageJournals(ctx, journalCutoffMS, now)
	service.deleteExpiredQuotaHistory(ctx, quotaHistoryCutoffMS, now)
	service.deleteExpiredRPM(ctx, requestLogCutoffMS, now)
}

// 额度历史独立保留 35 天，小时用量聚合仍长期保留。
func (service *Service) deleteExpiredQuotaHistory(ctx context.Context, cutoffMS int64, now time.Time) {
	for ctx.Err() == nil {
		var ids []uint
		if err := service.db.WithContext(ctx).Model(&models.CredentialQuotaHistory{}).
			Where("observed_at_ms < ?", cutoffMS).Order("observed_at_ms ASC").Limit(retentionBatchSize).Pluck("id", &ids).Error; err != nil {
			if ctx.Err() == nil {
				service.recordRetentionDeleteFailure(now)
			}
			return
		}
		if len(ids) == 0 {
			return
		}
		if err := service.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.CredentialQuotaHistory{}).Error; err != nil {
			if ctx.Err() == nil {
				service.recordRetentionDeleteFailure(now)
			}
			return
		}
	}
}

func retentionCutoffMS(nowMS int64, days int) (int64, error) {
	if days < 0 {
		return 0, fmt.Errorf("retention days must be non-negative")
	}
	retentionMS := int64(days) * epochms.MillisecondsPerDay
	if retentionMS > nowMS {
		return 0, nil
	}
	return nowMS - retentionMS, nil
}

func (service *Service) deleteExpiredRequestLogs(
	ctx context.Context,
	cutoffMS int64,
	now time.Time,
) bool {
	for {
		if ctx.Err() != nil {
			return false
		}

		var ids []string
		result := service.db.WithContext(ctx).
			Model(&models.RequestLog{}).
			Where("completed_at_ms < ?", cutoffMS).
			Order("completed_at_ms ASC").
			Order("id ASC").
			Limit(retentionBatchSize).
			Pluck("id", &ids)
		if result.Error != nil {
			if ctx.Err() == nil {
				service.recordRetentionDeleteFailure(now)
			}
			return false
		}
		if len(ids) == 0 {
			return true
		}

		result = service.db.WithContext(ctx).
			Where("id IN ?", ids).
			Delete(&models.RequestLog{})
		if result.Error != nil {
			if ctx.Err() == nil {
				service.recordRetentionDeleteFailure(now)
			}
			return false
		}
		if len(ids) < retentionBatchSize {
			return true
		}
	}
}

func (service *Service) deleteExpiredUsageJournals(
	ctx context.Context,
	cutoffMS int64,
	now time.Time,
) bool {
	for {
		if ctx.Err() != nil {
			return false
		}

		var requestIDs []string
		result := expiredUsageJournalBatch(service.db.WithContext(ctx), cutoffMS).
			Pluck("request_id", &requestIDs)
		if result.Error != nil {
			if ctx.Err() == nil {
				service.recordRetentionDeleteFailure(now)
			}
			return false
		}
		if len(requestIDs) == 0 {
			return true
		}

		result = service.db.WithContext(ctx).
			Where("request_id IN ?", requestIDs).
			Delete(&models.UsageAggregationJournal{})
		if result.Error != nil {
			if ctx.Err() == nil {
				service.recordRetentionDeleteFailure(now)
			}
			return false
		}
		if len(requestIDs) < retentionBatchSize {
			return true
		}
	}
}

// expiredUsageJournalBatch selects one batch of journal rows older than the
// cutoff. Naming both applied states lets the (applied, bucket_start_ms) index
// serve the range; filtering on bucket_start_ms alone scanned and sorted the
// whole journal on every sweep. Batch order does not matter because the cutoff
// is fixed for the whole sweep.
func expiredUsageJournalBatch(db *gorm.DB, cutoffMS int64) *gorm.DB {
	return db.Model(&models.UsageAggregationJournal{}).
		Where("applied IN ? AND bucket_start_ms < ?", []bool{false, true}, cutoffMS).
		Limit(retentionBatchSize)
}

func (service *Service) recordRetentionDeleteFailure(now time.Time) {
	service.retentionDeleteTotal.Add(1)
	service.recordRetentionFailureAt(now)
	service.warn("retention_delete_failure", 0)
}

func (service *Service) recordRetentionFailureAt(now time.Time) {
	service.statsMu.Lock()
	service.lastRetentionFailureAt = now
	service.statsMu.Unlock()
}
