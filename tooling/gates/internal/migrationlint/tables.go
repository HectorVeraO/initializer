package migrationlint

import (
	"regexp"
	"strings"
)

const (
	nameToken = `(?:"(?:[^"]|"")*"|[A-Za-z_\x{80}-\x{10FFFF}][A-Za-z0-9_$\x{80}-\x{10FFFF}]*)`
	qualified = nameToken + `(?: ?\. ?` + nameToken + `){0,2}`
)

var (
	qualifiedPattern = regexp.MustCompile(qualified)
	createTable      = regexp.MustCompile(`(?i)^CREATE (?:(?:GLOBAL |LOCAL )?(?:TEMPORARY |TEMP )|UNLOGGED )?TABLE (?:IF NOT EXISTS )?(` + qualified + `)`)
	selectStart      = regexp.MustCompile(`(?i)^(?:SELECT|WITH)\b`)
	intoTarget       = regexp.MustCompile(`(?i)\bINTO (?:(?:TEMPORARY |TEMP |UNLOGGED ))?(?:TABLE )?(` + qualified + `)`)
	insertBefore     = regexp.MustCompile(`(?i)\( ?(?:INSERT|MERGE) $`)
	alterTable       = regexp.MustCompile(`(?i)^ALTER TABLE (?:IF EXISTS )?(?:ONLY )?(` + qualified + `)`)
	createIndex      = regexp.MustCompile(`(?i)^CREATE (?:UNIQUE )?INDEX (?:CONCURRENTLY )?(?:IF NOT EXISTS )?(?:` + nameToken + ` )?ON (?:ONLY )?(` + qualified + `)`)
	references       = regexp.MustCompile(`(?i)\bREFERENCES\b ?(` + qualified + `)`)
	dropTable        = regexp.MustCompile(`(?i)^DROP TABLE (?:IF EXISTS )?(` + qualified + `(?: ?, ?` + qualified + `)*)`)
	renameTo         = regexp.MustCompile(`(?i)\bRENAME TO (` + qualified + `)`)

	renameColumn = regexp.MustCompile(`(?i)\bRENAME (?:COLUMN )?` + nameToken + ` TO\b`)
	dropColumn   = regexp.MustCompile(`(?i)\bDROP (?:(COLUMN) )?(?:IF EXISTS )?(` + nameToken + `)`)
	alterType    = regexp.MustCompile(`(?i)\bALTER (?:COLUMN )?` + nameToken + ` (?:SET DATA )?TYPE\b`)
	setNotNull   = regexp.MustCompile(`(?i)\bALTER (?:COLUMN )?` + nameToken + ` SET NOT NULL\b`)
	tableName    = regexp.MustCompile(`^[a-z]{2,4}_[a-z0-9_]+$`)
)

// checkStatement applies the table and destructive-change rules to one statement.
func (l *linter) checkStatement(file string, info *moduleInfo, statement Statement) {
	text := statement.Text
	var tables []string
	isAlter := false

	if m := createTable.FindStringSubmatch(text); m != nil {
		tables = append(tables, m[1])
	}
	// Top-level SELECT ... INTO creates a table; PL/pgSQL variable targets sit
	// in dollar-quoted bodies, which the scanner reduces to $$.
	if selectStart.MatchString(text) {
		for _, loc := range intoTarget.FindAllStringSubmatchIndex(text, -1) {
			isInsertOrMerge := insertBefore.MatchString(text[:loc[0]])
			if isInsertOrMerge {
				continue
			}
			tables = append(tables, text[loc[2]:loc[3]])
		}
	}
	if m := alterTable.FindStringSubmatch(text); m != nil {
		isAlter = true
		tables = append(tables, m[1])
		for _, rename := range renameTo.FindAllStringSubmatch(text, -1) {
			tables = append(tables, rename[1])
		}
	}
	if m := createIndex.FindStringSubmatch(text); m != nil {
		tables = append(tables, m[1])
	}
	if m := dropTable.FindStringSubmatch(text); m != nil {
		tables = append(tables, qualifiedPattern.FindAllString(m[1], -1)...)
	}
	for _, m := range references.FindAllStringSubmatch(text, -1) {
		tables = append(tables, m[1])
	}
	for _, table := range tables {
		l.checkTable(file, info, statement.Line, table)
	}

	var kinds []string
	if strings.HasPrefix(strings.ToUpper(text), "DROP TABLE ") {
		kinds = append(kinds, "DROP TABLE")
	}
	if isAlter {
		kinds = append(kinds, alterKinds(text)...)
	}
	if len(kinds) > 0 && !statement.ContractStep {
		l.add(RuleDestructive, file, statement.Line,
			"%s needs the line \"-- gatbase:contract-step <reason>\" directly above it", strings.Join(kinds, ", "))
	}
}

// otherDrops are the ALTER TABLE ... DROP subcommands that do not drop a column.
var otherDrops = map[string]bool{"CONSTRAINT": true, "DEFAULT": true, "NOT": true, "IDENTITY": true, "EXPRESSION": true}

// dropsColumn reports whether an ALTER TABLE statement drops a column, with or
// without the optional COLUMN keyword. The other DROP subcommands are skipped.
func dropsColumn(text string) bool {
	for _, m := range dropColumn.FindAllStringSubmatch(text, -1) {
		hasColumnKeyword := m[1] != ""
		isOtherSubcommand := otherDrops[strings.ToUpper(m[2])]
		if hasColumnKeyword || !isOtherSubcommand {
			return true
		}
	}
	return false
}

func alterKinds(text string) []string {
	var kinds []string
	if dropsColumn(text) {
		kinds = append(kinds, "DROP COLUMN")
	}
	if renameColumn.MatchString(text) {
		kinds = append(kinds, "RENAME COLUMN")
	}
	if renameTo.MatchString(text) {
		kinds = append(kinds, "RENAME TO")
	}
	if alterType.MatchString(text) {
		kinds = append(kinds, "ALTER COLUMN TYPE")
	}
	if setNotNull.MatchString(text) {
		kinds = append(kinds, "ALTER COLUMN SET NOT NULL")
	}
	return kinds
}

// splitQualified splits a possibly schema-qualified name on dots outside quotes.
func splitQualified(name string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
			current.WriteByte(c)
		case c == '.' && !inQuotes:
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	parts = append(parts, strings.TrimSpace(current.String()))
	return parts
}

func (l *linter) checkTable(file string, info *moduleInfo, line int, raw string) {
	parts := splitQualified(raw)
	if strings.Contains(raw, `"`) {
		l.add(RuleTableQuoted, file, line, "table identifier %s is quoted; use an unquoted lowercase name", raw)
		return
	}
	table := parts[len(parts)-1]
	schemas := parts[:len(parts)-1]
	if len(schemas) > 1 || (len(schemas) == 1 && !strings.EqualFold(schemas[0], "public")) {
		l.add(RuleTableSchemaInvalid, file, line, "table %s has a schema qualifier other than public", raw)
	}
	if !tableName.MatchString(table) {
		l.add(RuleTableNameInvalid, file, line, "table %q must match ^[a-z]{2,4}_[a-z0-9_]+$", table)
		return
	}
	if len(info.prefixes) == 0 {
		return
	}
	prefix, _, _ := strings.Cut(table, "_")
	if !info.prefixes[prefix] {
		l.add(RuleTablePrefixMismatch, file, line, "table %q has prefix %q, which module directory %s does not own", table, prefix, info.dir)
	}
}
