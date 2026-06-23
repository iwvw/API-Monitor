package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type tableInfo struct {
	Columns map[string]bool
	Rows    int64
}

type sourceSchema struct {
	Tables     map[string]map[string]bool
	References map[string]bool
}

type issue struct {
	Kind    string
	Table   string
	Details string
}

var (
	createTableRe = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	alterColumnRe = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?["']?([a-zA-Z_][a-zA-Z0-9_]*)`)
	tableRefRe    = regexp.MustCompile(`(?is)\b(?:FROM|JOIN|UPDATE|INTO)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	sqlShapeRe    = regexp.MustCompile(`(?is)^\s*(CREATE\s+(?:TABLE|INDEX|UNIQUE\s+INDEX)\b|ALTER\s+TABLE\b|SELECT\b[\s\S]*\bFROM\b|INSERT\b[\s\S]*\bINTO\b|UPDATE\b[\s\S]*\bSET\b|DELETE\s+FROM\b|WITH\b[\s\S]*\bSELECT\b|PRAGMA\b)`)
)

var retiredTableHints = []string{"music", "openlist", "chat_persona", "chat_session", "chat_message", "openlist"}
var optionalReferencedTables = map[string]bool{
	"chat_messages": true,
	"chat_sessions": true,
}

func main() {
	ctx := context.Background()
	root, err := findRepoRoot()
	if err != nil {
		exitf("find repo root: %v", err)
	}
	dbPath := resolveDBPath(root)
	source, err := inspectSource(filepath.Join(root, "backend-go", "internal"))
	if err != nil {
		exitf("inspect source: %v", err)
	}
	actual, err := inspectDatabase(ctx, dbPath)
	if err != nil {
		exitf("inspect database %s: %v", dbPath, err)
	}

	errors, warnings := compareSchemas(source, actual)
	printReport(dbPath, source, actual, errors, warnings)
	if len(errors) > 0 {
		os.Exit(1)
	}
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(wd, "package.json")) && exists(filepath.Join(wd, "backend-go")) {
			return wd, nil
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return "", fmt.Errorf("package.json + backend-go markers not found from %s", wd)
		}
		wd = parent
	}
}

func resolveDBPath(root string) string {
	dotenv := readDotEnv(filepath.Join(root, ".env"))
	if value := strings.TrimSpace(os.Getenv("DB_PATH")); value != "" {
		return absPath(root, value)
	}
	dataDir := firstNonEmpty(os.Getenv("DATA_DIR"), dotenv["DATA_DIR"], filepath.Join(root, "data"))
	dbName := firstNonEmpty(os.Getenv("DB_NAME"), dotenv["DB_NAME"], "data.db")
	return filepath.Join(absPath(root, dataDir), dbName)
}

func readDotEnv(path string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		values[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	}
	return values
}

func inspectSource(root string) (sourceSchema, error) {
	schema := sourceSchema{
		Tables:     map[string]map[string]bool{},
		References: map[string]bool{},
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, literal := range sqlStringLiterals(string(contentBytes)) {
			collectCreateTables(literal, schema.Tables)
			for _, match := range alterColumnRe.FindAllStringSubmatch(literal, -1) {
				table := normalizeName(match[1])
				column := normalizeName(match[2])
				ensureTable(schema.Tables, table)[column] = true
			}
			for _, match := range tableRefRe.FindAllStringSubmatch(literal, -1) {
				table := normalizeName(match[1])
				if !isSQLKeyword(table) && !isSystemTable(table) {
					schema.References[table] = true
				}
			}
		}
		return nil
	})
	return schema, err
}

func sqlStringLiterals(content string) []string {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("source.go", fileSet.Base(), len(content))
	var scan scanner.Scanner
	scan.Init(file, []byte(content), nil, scanner.ScanComments)

	literals := []string{}
	for {
		_, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.STRING {
			continue
		}
		value, err := strconv.Unquote(lit)
		if err != nil || !looksLikeSQL(value) {
			continue
		}
		literals = append(literals, value)
	}
	return literals
}

func looksLikeSQL(value string) bool {
	return sqlShapeRe.MatchString(value)
}

func collectCreateTables(content string, tables map[string]map[string]bool) {
	matches := createTableRe.FindAllStringSubmatchIndex(content, -1)
	for _, match := range matches {
		table := normalizeName(content[match[2]:match[3]])
		openParen := match[1] - 1
		body, ok := parenBody(content, openParen)
		if !ok {
			continue
		}
		columns := ensureTable(tables, table)
		for _, definition := range splitTopLevel(body, ',') {
			column, ok := columnName(definition)
			if ok {
				columns[column] = true
			}
		}
	}
}

func inspectDatabase(ctx context.Context, dbPath string) (map[string]tableInfo, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := map[string]tableInfo{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		info := tableInfo{Columns: map[string]bool{}}
		info.Rows = countRows(ctx, db, name)
		columns, err := tableColumns(ctx, db, name)
		if err != nil {
			return nil, err
		}
		info.Columns = columns
		tables[normalizeName(name)] = info
	}
	return tables, rows.Err()
}

func tableColumns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+quoteIdentifier(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[normalizeName(name)] = true
	}
	return columns, rows.Err()
}

func countRows(ctx context.Context, db *sql.DB, table string) int64 {
	var count int64
	_ = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoteIdentifier(table)).Scan(&count)
	return count
}

func compareSchemas(source sourceSchema, actual map[string]tableInfo) ([]issue, []issue) {
	errors := []issue{}
	warnings := []issue{}

	expectedTables := map[string]bool{}
	for table := range source.Tables {
		expectedTables[table] = true
	}
	for table := range source.References {
		expectedTables[table] = true
	}

	for table, columns := range source.Tables {
		actualTable, ok := actual[table]
		if !ok {
			errors = append(errors, issue{Kind: "missing_table", Table: table, Details: "declared by backend schema but absent from database"})
			continue
		}
		missingColumns := []string{}
		for column := range columns {
			if !actualTable.Columns[column] {
				missingColumns = append(missingColumns, column)
			}
		}
		if len(missingColumns) > 0 {
			sort.Strings(missingColumns)
			errors = append(errors, issue{Kind: "missing_columns", Table: table, Details: strings.Join(missingColumns, ", ")})
		}
	}

	for table := range source.References {
		_, actualOK := actual[table]
		_, declaredOK := source.Tables[table]
		if !actualOK && !declaredOK {
			item := issue{Kind: "missing_referenced_table", Table: table, Details: "referenced by backend SQL but absent from database and schema declarations"}
			if optionalReferencedTables[table] {
				item.Kind = "optional_legacy_table_absent"
				item.Details = "optional legacy compatibility table is absent"
				warnings = append(warnings, item)
				continue
			}
			errors = append(errors, item)
		}
	}

	for table, info := range actual {
		if isRetiredTable(table) {
			warnings = append(warnings, issue{Kind: "retired_table_suspect", Table: table, Details: fmt.Sprintf("%d rows", info.Rows)})
			continue
		}
		if !expectedTables[table] {
			warnings = append(warnings, issue{Kind: "extra_table", Table: table, Details: fmt.Sprintf("%d rows; not referenced by current Go backend", info.Rows)})
		}
	}

	sortIssues(errors)
	sortIssues(warnings)
	return errors, warnings
}

func printReport(dbPath string, source sourceSchema, actual map[string]tableInfo, errors, warnings []issue) {
	fmt.Println("Database schema audit")
	fmt.Println("DB:", dbPath)
	fmt.Printf("Source-declared tables: %d\n", len(source.Tables))
	fmt.Printf("Backend-referenced tables: %d\n", len(source.References))
	fmt.Printf("Database tables: %d\n", len(actual))
	fmt.Println()

	if len(errors) == 0 {
		fmt.Println("Errors: none")
	} else {
		fmt.Printf("Errors: %d\n", len(errors))
		for _, item := range errors {
			fmt.Printf("  - [%s] %s: %s\n", item.Kind, item.Table, item.Details)
		}
	}

	if len(warnings) == 0 {
		fmt.Println("Warnings: none")
	} else {
		fmt.Printf("Warnings: %d\n", len(warnings))
		for _, item := range warnings {
			fmt.Printf("  - [%s] %s: %s\n", item.Kind, item.Table, item.Details)
		}
	}

	fmt.Println()
	fmt.Println("Tables:")
	for _, table := range sortedKeysInfo(actual) {
		fmt.Printf("  - %s: %d rows, %d columns\n", table, actual[table].Rows, len(actual[table].Columns))
	}
}

func ensureTable(tables map[string]map[string]bool, table string) map[string]bool {
	if tables[table] == nil {
		tables[table] = map[string]bool{}
	}
	return tables[table]
}

func parenBody(content string, openParen int) (string, bool) {
	if openParen < 0 || openParen >= len(content) || content[openParen] != '(' {
		return "", false
	}
	depth := 0
	start := openParen + 1
	for i := openParen; i < len(content); i++ {
		switch content[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return content[start:i], true
			}
		}
	}
	return "", false
}

func splitTopLevel(value string, separator rune) []string {
	parts := []string{}
	depth := 0
	start := 0
	for index, r := range value {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if r == separator && depth == 0 {
				parts = append(parts, value[start:index])
				start = index + len(string(r))
			}
		}
	}
	parts = append(parts, value[start:])
	return parts
}

func columnName(definition string) (string, bool) {
	definition = strings.TrimSpace(definition)
	definition = strings.Trim(definition, "`")
	if definition == "" {
		return "", false
	}
	fields := strings.Fields(definition)
	if len(fields) == 0 {
		return "", false
	}
	first := normalizeName(strings.Trim(fields[0], `"'[]`))
	firstHead := first
	if index := strings.Index(firstHead, "("); index >= 0 {
		firstHead = firstHead[:index]
	}
	if first == "" || isColumnConstraint(first) {
		return "", false
	}
	if isColumnConstraint(firstHead) {
		return "", false
	}
	return first, true
}

func isColumnConstraint(value string) bool {
	switch strings.ToLower(value) {
	case "primary", "foreign", "unique", "check", "constraint", "key", "index":
		return true
	default:
		return false
	}
}

func isSQLKeyword(value string) bool {
	switch strings.ToLower(value) {
	case "select", "where", "set", "values", "on", "do", "returning", "as", "excluded":
		return true
	default:
		return false
	}
}

func isSystemTable(value string) bool {
	switch strings.ToLower(value) {
	case "sqlite_master", "sqlite_sequence", "dbstat":
		return true
	default:
		return false
	}
}

func isRetiredTable(table string) bool {
	for _, hint := range retiredTableHints {
		if strings.Contains(table, hint) {
			return true
		}
	}
	return false
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func normalizeName(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.Trim(value, `"`)))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func absPath(root, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, value)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sortedKeysInfo(values map[string]tableInfo) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortIssues(values []issue) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Kind != values[j].Kind {
			return values[i].Kind < values[j].Kind
		}
		return values[i].Table < values[j].Table
	})
}

func exitf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
