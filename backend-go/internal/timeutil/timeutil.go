package timeutil

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// 站点时区设置保存在 user_settings.time_zone（单行常量表 id=1），
// 取值 system（跟随服务器本地）/UTC/IANA 名称等。
// 本包是全站唯一的时区换算门面，各业务（调度器、AI、统计、通知）统一从这里取。

// LocationFromSettings 读取设置中的时区并返回对应 *time.Location。
// 约定（全站统一）：
//   - time_zone 为 "" 或 "system"，或查询失败：跟随服务器本地时区 time.Local；
//   - 其余值为 IANA 名称（如 Asia/Shanghai），加载失败回退服务器本地时区。
func LocationFromSettings(ctx context.Context, db *sql.DB) *time.Location {
	return LocationFromName(ReadTimeZone(ctx, db))
}

// LocationFromName 把时区名解析为 location，遵循与 LocationFromSettings 相同的约定。
func LocationFromName(zone string) *time.Location {
	name := strings.TrimSpace(zone)
	if name == "" || name == "system" {
		return time.Local
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}

// ReadTimeZone 读取 user_settings 表里的时区设置字符串；查询失败返回 "system"。
func ReadTimeZone(ctx context.Context, db *sql.DB) string {
	var zone sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT time_zone FROM user_settings WHERE id = 1`).Scan(&zone); err != nil || !zone.Valid {
		return "system"
	}
	return strings.TrimSpace(zone.String)
}

// FormatInLocation 把时间按指定时区格式化为 RFC3339 时间戳（带时区偏移）。
func FormatInLocation(t time.Time, loc *time.Location) string {
	return t.In(loc).Format(time.RFC3339)
}