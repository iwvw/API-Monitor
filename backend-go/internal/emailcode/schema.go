package emailcode

import (
	"context"
	"database/sql"
	"fmt"
)

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS emailcode_messages (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id     TEXT NOT NULL DEFAULT '',
			mailbox        TEXT NOT NULL,
			domain         TEXT NOT NULL DEFAULT '',
			sender         TEXT NOT NULL DEFAULT '',
			from_domain    TEXT NOT NULL DEFAULT '',
			subject        TEXT NOT NULL DEFAULT '',
			code           TEXT NOT NULL DEFAULT '',
			link           TEXT NOT NULL DEFAULT '',
			snippet        TEXT NOT NULL DEFAULT '',
			text_body      TEXT NOT NULL DEFAULT '',
			extract_status TEXT NOT NULL DEFAULT '',
			received_at    TEXT NOT NULL,
			consumed_at    TEXT,
			consumed_by    TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS emailcode_daily_stats (
			day          TEXT NOT NULL,
			domain       TEXT NOT NULL,
			from_domain  TEXT NOT NULL,
			received     INTEGER NOT NULL DEFAULT 0,
			extracted    INTEGER NOT NULL DEFAULT 0,
			primary_spans INTEGER NOT NULL DEFAULT 0,
			updated_at   TEXT NOT NULL,
			PRIMARY KEY (day, domain, from_domain)
		)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure emailcode schema: %w", err)
		}
	}
	// 兼容早期建表：补列，老库无需重建。必须在建索引之前完成，
	// 否则引用新列的唯一索引会在老库上失败（no such column）。
	columns := map[string]string{
		"message_id":     "TEXT NOT NULL DEFAULT ''",
		"from_domain":    "TEXT NOT NULL DEFAULT ''",
		"text_body":      "TEXT NOT NULL DEFAULT ''",
		"extract_status": "TEXT NOT NULL DEFAULT ''",
		"consumed_by":    "TEXT NOT NULL DEFAULT ''",
	}
	for name, def := range columns {
		if err := ensureColumn(ctx, db, "emailcode_messages", name, def); err != nil {
			return err
		}
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_emailcode_messages_mailbox ON emailcode_messages(mailbox, id)`,
		`CREATE INDEX IF NOT EXISTS idx_emailcode_messages_consumed ON emailcode_messages(consumed_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_emailcode_messages_msgid ON emailcode_messages(message_id) WHERE message_id <> ''`,
	}
	for _, stmt := range indexes {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ensure emailcode index: %w", err)
		}
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table, name, definition string) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, pk int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if columnName == name {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+name+` `+definition)
	return err
}
