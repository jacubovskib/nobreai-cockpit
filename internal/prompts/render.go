// Package prompts renderiza prompts salvos com variáveis {{ nome }}.
package prompts

import (
	"regexp"
	"strings"
)

// Vars mapeia o nome da variável ao valor.
type Vars map[string]string

// KnownVars são as variáveis aceitas em prompts salvos.
var KnownVars = []string{
	"card.id", "card.titulo", "card.onde_parei", "card.pacote", "card.arquivos",
	"card.como_testar", "git.branch", "handoff.faltando",
}

// UnknownVarsError lista as variáveis desconhecidas, na ordem da primeira aparição.
type UnknownVarsError struct{ Names []string }

func (e *UnknownVarsError) Error() string {
	return "variáveis desconhecidas: " + strings.Join(e.Names, ", ")
}

var (
	placeholder = regexp.MustCompile(`\{\{([^{}]*)\}\}`)
	validName   = regexp.MustCompile(`^[a-z_]+(\.[a-z_]+)*$`)
	known       = func() map[string]bool {
		m := map[string]bool{}
		for _, n := range KnownVars {
			m[n] = true
		}
		return m
	}()
)

// Render substitui {{ nome }} (espaços opcionais) pelos valores de v. Chaves
// cujo conteúdo não é um nome válido ficam intactas. Se algum nome válido não
// é conhecido, devolve *UnknownVarsError sem renderizar parcialmente.
func Render(body string, v Vars) (string, error) {
	var unknown []string
	seen := map[string]bool{}
	for _, m := range placeholder.FindAllStringSubmatch(body, -1) {
		name := strings.TrimSpace(m[1])
		if validName.MatchString(name) && !known[name] && !seen[name] {
			seen[name] = true
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		return "", &UnknownVarsError{Names: unknown}
	}
	out := placeholder.ReplaceAllStringFunc(body, func(s string) string {
		name := strings.TrimSpace(s[2 : len(s)-2])
		if !validName.MatchString(name) {
			return s
		}
		return v[name]
	})
	return out, nil
}
