package storage

import (
	"fmt"
	"os"
	"testing"

	"gorm.io/gorm"
)

func TestGroupNamePerChannelMigrationContract(t *testing.T) {
	testGroupNamePerChannelMigration(t, openInternalMigrationTestDatabase)
}

func TestExternalGroupNamePerChannelMigrationContract(t *testing.T) {
	dsn := os.Getenv("GPT_LOAD_DATABASE_TEST_DSN")
	if dsn == "" {
		t.Skip("GPT_LOAD_DATABASE_TEST_DSN is not set")
	}
	testGroupNamePerChannelMigration(t, func(t *testing.T) *gorm.DB { return openExternalIncrementalMigrationDatabase(t, dsn) })
}

func insertMigrationTestGroup(db *gorm.DB, id int, name, channelID string) error {
	return db.Table("groups").Create(map[string]any{
		"id": id, "name": name, "channel_id": channelID, "connection_type": "api_key",
		"params": "{}", "models": "[]", "enabled": true, "created_at_ms": 0, "updated_at_ms": 0,
	}).Error
}

func testGroupNamePerChannelMigration(t *testing.T, open func(*testing.T) *gorm.DB) {
	for _, scenario := range []string{"fresh", "upgrade", "interrupted", "partial"} {
		t.Run(scenario, func(t *testing.T) {
			db := open(t)
			if len(migrations) < 33 {
				t.Fatal("group name per channel migration is missing")
			}
			if scenario != "fresh" {
				if err := applyMigrationRegistry(db, migrations[:32]); err != nil {
					t.Fatal(err)
				}
				if !db.Migrator().HasIndex("groups", "idx_groups_name") {
					t.Fatal("pre-migration global group name index is missing")
				}
				if err := insertMigrationTestGroup(db, 1, "jack", "anthropic"); err != nil {
					t.Fatal(err)
				}
				if err := insertMigrationTestGroup(db, 2, "other", "codex"); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "interrupted":
				entry := migrations[32]
				up := entry.Up
				entry.Up = func(tx *gorm.DB) error {
					if err := up(tx); err != nil {
						return err
					}
					return fmt.Errorf("interrupt group name per channel migration")
				}
				registry := append(append([]migration(nil), migrations[:32]...), entry)
				if err := applyMigrationRegistry(db, registry); err == nil {
					t.Fatal("expected migration interruption")
				}
			case "partial":
				// MySQL 的非事务 DDL 可能在建立复合索引后、删除全局索引前中断。
				if err := db.Exec(`CREATE UNIQUE INDEX idx_groups_channel_name ON ` +
					quoteMigrationTestTable(db, "groups") + ` (channel_id, name)`).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := AutoMigrate(db); err != nil {
				t.Fatal(err)
			}
			if db.Migrator().HasIndex("groups", "idx_groups_name") {
				t.Fatal("global group name index remains")
			}
			if !db.Migrator().HasIndex("groups", "idx_groups_channel_name") {
				t.Fatal("group channel name index is missing")
			}
			if scenario == "fresh" {
				if err := insertMigrationTestGroup(db, 1, "jack", "anthropic"); err != nil {
					t.Fatal(err)
				}
			} else {
				var names []string
				if err := db.Table("groups").Order("id ASC").Pluck("name", &names).Error; err != nil {
					t.Fatal(err)
				}
				if len(names) != 2 || names[0] != "jack" || names[1] != "other" {
					t.Fatalf("migrated group names = %v", names)
				}
			}
			if err := insertMigrationTestGroup(db, 3, "jack", "codex"); err != nil {
				t.Fatalf("same name on another channel: %v", err)
			}
			if err := insertMigrationTestGroup(db, 4, "jack", "anthropic"); err == nil {
				t.Fatal("same name on the same channel was accepted")
			}
			// 重启时已登记迁移的冻结校验仍须通过。
			if err := AutoMigrate(db); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGroupNamePerChannelMigrationRejectsMissingUniqueness(t *testing.T) {
	db := openInternalMigrationTestDatabase(t)
	if err := applyMigrationRegistry(db, migrations[:32]); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`DROP INDEX idx_groups_name`).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err == nil {
		t.Fatal("migration accepted a groups table without any name uniqueness index")
	}
}

func quoteMigrationTestTable(db *gorm.DB, table string) string {
	if db.Dialector.Name() == "mysql" {
		return "`" + table + "`"
	}
	return `"` + table + `"`
}
