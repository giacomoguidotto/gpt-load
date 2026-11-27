package storage

import (
	"testing"

	"gorm.io/gorm"
)

// insertOrphanCostLimitRule writes a row whose access key does not exist, as a
// pre-existing integrity fault the post-migration check would report.
func insertOrphanCostLimitRule(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		`INSERT INTO access_key_cost_limit_rules
			(access_key_id, kind, limit_nano_usd, period_seconds, rule_revision, created_at_ms, updated_at_ms)
		VALUES (999, 'total', 1, 0, 1, 1, 1)`,
	).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		t.Fatal(err)
	}
}

func TestStartupWithoutPendingMigrationsSkipsForeignKeyScan(t *testing.T) {
	t.Parallel()
	db := openInternalMigrationTestDatabase(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	insertOrphanCostLimitRule(t, db)

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("up-to-date startup scanned foreign keys: %v", err)
	}
}

func TestStartupWithPendingMigrationsChecksForeignKeys(t *testing.T) {
	t.Parallel()
	db := openInternalMigrationTestDatabase(t)
	if err := applyMigrationRegistry(db, migrations[:len(migrations)-1]); err != nil {
		t.Fatal(err)
	}
	insertOrphanCostLimitRule(t, db)

	if err := AutoMigrate(db); err == nil {
		t.Fatal("startup that applied a migration accepted a foreign key violation")
	}
}
