package render

import (
	"os"

	"github.com/nakamasato/tfreview/internal/model"
)

// palette is either the ANSI escapes or empty strings, so every caller can wrap
// unconditionally and colour stays a single branch taken once per run.
type palette struct{ on bool }

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
)

func (p palette) wrap(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return code + s + ansiReset
}

func (p palette) bold(s string) string { return p.wrap(ansiBold, s) }
func (p palette) dim(s string) string  { return p.wrap(ansiDim, s) }

func (p palette) action(a, s string) string {
	switch a {
	case "create":
		return p.wrap(ansiGreen, s)
	case "delete":
		return p.wrap(ansiRed, s)
	case "update":
		return p.wrap(ansiYellow, s)
	default: // replace, and anything a future terraform adds
		return p.wrap(ansiMagenta, s)
	}
}

func (p palette) verdict(v model.VerdictKind, s string) string {
	switch v {
	case model.VerdictHit:
		return p.wrap(ansiBold+ansiRed, s)
	case model.VerdictUnverifiable:
		return p.wrap(ansiYellow, s)
	default: // miss, skipped
		return p.dim(s)
	}
}

func (p palette) severity(sv model.Severity, s string) string {
	switch sv {
	case model.SeverityCritical:
		return p.wrap(ansiRed, s)
	case model.SeverityHigh:
		return p.wrap(ansiYellow, s)
	case model.SeverityMedium:
		return p.wrap(ansiCyan, s)
	default:
		return p.dim(s)
	}
}

// ColorEnabled reports whether stdout is a terminal that wants colour. Redirected
// output must stay plain: the debug format is routinely piped to grep or a file.
func ColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
