package requestlog

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"gpt-load/internal/storage/models"
)

// An attempt filter must not be evaluated once per request row: on a
// multi-million-row log that plan never finishes and holds SQLite's only
// connection.
func TestAttemptFiltersAreNotCorrelatedPerRequestRow(t *testing.T) {
	db := openRequestLogQueryDB(t)
	groupID, credentialID := uint(12), uint(7)
	statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var total int64
		return applyAttemptFilters(tx.Model(&models.RequestLog{}), ListQuery{
			GroupID: &groupID, CredentialID: &credentialID,
		}, false).Count(&total)
	})

	var plan []struct {
		Detail string `gorm:"column:detail"`
	}
	if err := db.Raw("EXPLAIN QUERY PLAN " + statement).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, step := range plan {
		// SQLite labels a per-row subquery "CORRELATED" or, when it rewrites
		// the subquery as a semi-join, "EXISTS".
		if strings.Contains(step.Detail, "CORRELATED") || strings.Contains(step.Detail, "EXISTS") {
			t.Fatalf("attempt filter runs a correlated subquery per request: %q", step.Detail)
		}
	}
}

// The hourly journal sweep must seek the expired range through an index; a
// full scan of millions of journal rows held SQLite's only connection for
// seconds every hour even when nothing had expired.
func TestUsageJournalSweepSeeksExpiredRangeByIndex(t *testing.T) {
	db := openRequestLogQueryDB(t)
	statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var ids []string
		return expiredUsageJournalBatch(tx, 1_700_000_000_000).Pluck("request_id", &ids)
	})

	var plan []struct {
		Detail string `gorm:"column:detail"`
	}
	if err := db.Raw("EXPLAIN QUERY PLAN " + statement).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, step := range plan {
		if strings.HasPrefix(step.Detail, "SCAN") || strings.Contains(step.Detail, "TEMP B-TREE") {
			t.Fatalf("journal sweep does not seek by index: %q", step.Detail)
		}
	}
}

// A filtered page must be read in page order and stop after one page; sorting
// every matching request first took 31 s for one busy group.
func TestAttemptFilteredPageWalksCompletionOrder(t *testing.T) {
	db := openRequestLogQueryDB(t)
	groupID := uint(12)
	statement := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []models.RequestLog
		return applyAttemptFilters(tx.Model(&models.RequestLog{}), ListQuery{GroupID: &groupID}, true).
			Order("completed_at_ms DESC").Order("id DESC").Limit(20).Find(&rows)
	})

	var plan []struct {
		Detail string `gorm:"column:detail"`
	}
	if err := db.Raw("EXPLAIN QUERY PLAN " + statement).Scan(&plan).Error; err != nil {
		t.Fatal(err)
	}
	for _, step := range plan {
		if strings.Contains(step.Detail, "TEMP B-TREE FOR ORDER BY") {
			t.Fatalf("filtered page sorts every match: %q", step.Detail)
		}
	}
}
