package storage

import (
	"context"
	"testing"
	"time"
)

// A cancelled request must stop its SQLite statement. SQLite has one
// connection here, so a statement that ignores cancellation blocks every
// other query until it finishes on its own.
func TestCancelledContextInterruptsRunningSelect(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	var count int64
	// The unbounded recursive CTE only ends when SQLite is interrupted.
	err = db.WithContext(ctx).Raw(
		"WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM n) SELECT count(*) FROM n",
	).Scan(&count).Error
	if err == nil {
		t.Fatal("unbounded query returned without error")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancelled query ran for %s", elapsed)
	}

	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("connection unusable after interrupt: %v", err)
	}
}
