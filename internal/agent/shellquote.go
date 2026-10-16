package agent

import "strings"

// shellQuote wraps s in single quotes for safe interpolation into a command
// line executed by a POSIX shell (attach commands run via `bash -c`). Embedded
// single quotes are escaped as `'\”`. Callers pass literal guest paths, so no
// expansion is intended.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
