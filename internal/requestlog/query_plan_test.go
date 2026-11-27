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
		}).Count(&total)
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
