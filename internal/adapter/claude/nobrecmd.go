package claude

import "strings"

// plainNobreCommand diz se cmd é uma única chamada do nobre, sem nada que o
// shell execute além dela. É o que a pausa por limite deixa passar: o agente
// registra o handoff, mas "nobre status && make test" não fura o bloqueio.
//
// Fora de aspas, recusa ; & | < > quebra de linha, crase e $(. Entre aspas
// duplas, recusa crase e $( (o shell ainda os executa). Entre aspas simples,
// tudo é texto. Aspas sem fechar também são recusadas.
func plainNobreCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if !strings.HasPrefix(cmd, "nobre ") {
		return false
	}
	const (
		bare = iota
		single
		double
	)
	state := bare
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch state {
		case single:
			if c == '\'' {
				state = bare
			}
		case double:
			switch {
			case c == '\\':
				i++ // \" e \\ não fecham nem abrem nada
			case c == '"':
				state = bare
			case c == '`', c == '$' && i+1 < len(cmd) && cmd[i+1] == '(':
				return false
			}
		default:
			switch {
			case c == '\\':
				i++
			case c == '\'':
				state = single
			case c == '"':
				state = double
			case strings.IndexByte(";&|<>\n\r`", c) >= 0, c == '$' && i+1 < len(cmd) && cmd[i+1] == '(':
				return false
			}
		}
	}
	return state == bare
}
