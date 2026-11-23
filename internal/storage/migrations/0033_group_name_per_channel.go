package migrations

import (
	"fmt"

	"gorm.io/gorm"
)

const ID0033 = "0033_group_name_per_channel"

const (
	groupNameIndex0033        = "idx_groups_name"
	groupChannelNameIndex0033 = "idx_groups_channel_name"
)

type group0033 struct {
	ChannelID string `gorm:"type:varchar(64);not null;uniqueIndex:idx_groups_channel_name,priority:1"`
	Name      string `gorm:"type:varchar(255);not null;uniqueIndex:idx_groups_channel_name,priority:2"`
}

func (group0033) TableName() string { return "groups" }

type groupIndexColumn0033 struct {
	Name     string
	IsUnique bool
	IsValid  bool
}

// Up0033 将分组名唯一性从全局收窄到同一渠道内。先建复合唯一索引再删除全局索引，
// 任一中断点都至少保留一条唯一约束；全局索引存在时既有名称必然满足复合约束。
func Up0033(db *gorm.DB) error {
	if err := ValidateRecoverable0033(db); err != nil {
		return err
	}
	if !db.Migrator().HasIndex(&group0033{}, groupChannelNameIndex0033) {
		if err := db.Migrator().CreateIndex(&group0033{}, groupChannelNameIndex0033); err != nil {
			return fmt.Errorf("create group channel name index: %w", err)
		}
	}
	if db.Migrator().HasIndex(&group0033{}, groupNameIndex0033) {
		if err := db.Migrator().DropIndex(&group0033{}, groupNameIndex0033); err != nil {
			return fmt.Errorf("drop global group name index: %w", err)
		}
	}
	return Validate0033(db)
}

// ValidateRecoverable0033 接受迁移前、复合索引已建立（MySQL 非事务 DDL 中断）
// 与迁移完成三种状态；两条唯一约束都缺失时拒绝继续。
func ValidateRecoverable0033(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "sqlite", "mysql", "postgres":
	default:
		return fmt.Errorf("unsupported group name migration driver %q", db.Dialector.Name())
	}
	if !db.Migrator().HasTable(&group0033{}) {
		return fmt.Errorf("groups table is missing")
	}
	hasComposite := db.Migrator().HasIndex(&group0033{}, groupChannelNameIndex0033)
	if hasComposite {
		return validateGroupChannelNameIndex0033(db)
	}
	if !db.Migrator().HasIndex(&group0033{}, groupNameIndex0033) {
		return fmt.Errorf("group name uniqueness index is missing")
	}
	return nil
}

func Validate0033(db *gorm.DB) error {
	if err := validateGroupChannelNameIndex0033(db); err != nil {
		return err
	}
	if db.Migrator().HasIndex(&group0033{}, groupNameIndex0033) {
		return fmt.Errorf("global group name index remains")
	}
	return nil
}

// groupNameIndexRetired0033 表示全局分组名索引已由 0033 的复合索引取代，
// 供冻结的初始结构校验在升级后跳过该索引。
func groupNameIndexRetired0033(db *gorm.DB, table, index string) bool {
	return table == "groups" && index == groupNameIndex0033 &&
		db.Migrator().HasIndex(&group0033{}, groupChannelNameIndex0033)
}

func validateGroupChannelNameIndex0033(db *gorm.DB) error {
	columns, err := groupChannelNameIndexColumns0033(db)
	if err != nil {
		return err
	}
	want := []string{"channel_id", "name"}
	if len(columns) != len(want) {
		return fmt.Errorf("group channel name index is missing or has an unexpected definition")
	}
	for position, column := range columns {
		if column.Name != want[position] || !column.IsUnique || !column.IsValid {
			return fmt.Errorf("group channel name index has an unexpected definition")
		}
	}
	return nil
}

func groupChannelNameIndexColumns0033(db *gorm.DB) ([]groupIndexColumn0033, error) {
	var columns []groupIndexColumn0033
	var query string
	var arguments []any
	switch db.Dialector.Name() {
	case "postgres":
		query = `
			SELECT attribute.attname AS name,
				definition.indisunique AS is_unique, definition.indisvalid AS is_valid
			FROM pg_index AS definition
			JOIN pg_class AS table_relation ON table_relation.oid = definition.indrelid
			JOIN pg_namespace AS namespace ON namespace.oid = table_relation.relnamespace
			JOIN pg_class AS index_relation ON index_relation.oid = definition.indexrelid
			CROSS JOIN LATERAL unnest(definition.indkey) WITH ORDINALITY AS key(attnum, position)
			JOIN pg_attribute AS attribute ON attribute.attrelid = table_relation.oid
				AND attribute.attnum = key.attnum
			WHERE namespace.nspname = current_schema() AND table_relation.relname = 'groups'
				AND index_relation.relname = ?
			ORDER BY key.position`
		arguments = []any{groupChannelNameIndex0033}
	case "mysql":
		query = `
			SELECT column_name AS name, non_unique = 0 AS is_unique, 1 AS is_valid
			FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = 'groups' AND index_name = ?
			ORDER BY seq_in_index`
		arguments = []any{groupChannelNameIndex0033}
	case "sqlite":
		query = `
			SELECT column_info.name AS name, index_list."unique" <> 0 AS is_unique, 1 AS is_valid
			FROM pragma_index_info(?) AS column_info
			JOIN pragma_index_list('groups') AS index_list ON index_list.name = ?
			ORDER BY column_info.seqno`
		arguments = []any{groupChannelNameIndex0033, groupChannelNameIndex0033}
	default:
		return nil, fmt.Errorf("group channel name index: unsupported database driver %q", db.Dialector.Name())
	}
	if err := db.Raw(query, arguments...).Scan(&columns).Error; err != nil {
		return nil, fmt.Errorf("read group channel name index: %w", err)
	}
	return columns, nil
}
