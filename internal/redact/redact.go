// Package redact removes credentials before operational messages reach storage.
package redact

import (
	"regexp"
	"sort"
	"strings"
)

const replacement = "[REDACTED]"

var patterns = []struct {
	expression  *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)(PVEAPIToken\s*=\s*)([^\s]+)`), `${1}` + replacement},
	{regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)([^\s]+)`), `${1}` + replacement},
	{regexp.MustCompile(`(?i)(Bearer\s+)([A-Za-z0-9._~+/=-]+)`), `${1}` + replacement},
	{regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[-_ ]?key)(\s*[=:]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`), `${1}${2}` + replacement},
	{regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----`), replacement},
}

// Text masks common credential formats and any explicitly supplied secret values.
func Text(message string, secrets ...string) string {
	for _, pattern := range patterns {
		message = pattern.expression.ReplaceAllString(message, pattern.replacement)
	}
	values := append([]string(nil), secrets...)
	sort.SliceStable(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, secret := range values {
		if len(secret) >= 4 {
			message = strings.ReplaceAll(message, secret, replacement)
		}
	}
	return message
}
