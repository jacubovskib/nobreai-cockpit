package pty

import "regexp"

var allowedEnvRe = regexp.MustCompile(`^((ANTHROPIC_|CLAUDE_|AWS_|GOOGLE_|VERTEX_|OTEL_)[A-Z0-9_]*|LC_[A-Za-z0-9_]*)$`)

var allowedEnvExact = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true,
	"LANG": true, "TZ": true, "COLORTERM": true,
}

// AllowedEnv diz se o `nobre run` pode repassar a variável ao terminal do
// agente. O daemon revalida com a mesma regra (nunca PATH, LD_*, NOBRE_*).
func AllowedEnv(name string) bool { return allowedEnvExact[name] || allowedEnvRe.MatchString(name) }
