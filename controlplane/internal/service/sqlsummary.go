package service

import (
	"regexp"
	"strings"
)

var (
	sqlQuoted   = regexp.MustCompile(`'(?:[^']|'')*'`)
	sqlComments = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
	sqlVerb     = regexp.MustCompile(`(?i)^\s*(insert\s+or\s+\w+\s+into|insert\s+into|replace\s+into|update(?:\s+or\s+\w+)?|delete\s+from|create\s+(?:unique\s+)?(?:table|index|view|trigger)(?:\s+if\s+not\s+exists)?|alter\s+table|drop\s+(?:table|index|view|trigger)(?:\s+if\s+exists)?)\s+([A-Za-z_][\w.]*|"[^"]{1,64}"|` + "`[^`]{1,64}`" + `|\[[^\]]{1,64}\])`)
)

// sqlSummary names what a SQL write did, such as "UPDATE on orders" or
// "INSERT into users, DELETE from sessions", without a single value: quoted
// text and comments go first, and only keywords and table names stay.
func sqlSummary(sql string) string {
	sql = sqlComments.ReplaceAllString(sqlQuoted.ReplaceAllString(sql, "''"), " ")
	var parts []string
	for _, statement := range strings.Split(sql, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if len(parts) == 3 {
			parts = append(parts, "more")
			break
		}
		match := sqlVerb.FindStringSubmatch(statement)
		if match == nil {
			parts = append(parts, "SQL")
			continue
		}
		words := strings.Fields(strings.ToUpper(match[1]))
		verb := words[0]
		table := strings.Trim(match[2], "\"`[]")
		switch verb {
		case "INSERT", "REPLACE":
			parts = append(parts, verb+" into "+table)
		case "DELETE":
			parts = append(parts, "DELETE from "+table)
		case "UPDATE":
			parts = append(parts, "UPDATE on "+table)
		default:
			parts = append(parts, verb+" "+strings.ToLower(words[1])+" "+table)
		}
	}
	return clipText(strings.Join(parts, ", "), 200)
}
