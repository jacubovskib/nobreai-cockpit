package claude

import "strings"

// handoffSubcommands são os subcomandos que a pausa por limite deixa passar:
// os que registram o handoff no card. run (executa outro programa), setup,
// hook, ui, project e prompt ficam de fora.
var handoffSubcommands = map[string]bool{
	"start": true, "card": true, "resume": true, "note": true, "decide": true, "file": true,
	"todo": true, "ask": true, "answer": true, "branch": true, "app": true, "block": true, "test": true,
	"done": true, "status": true, "help": true,
}

// plainNobreCommand diz se cmd é uma única chamada de um subcomando de handoff
// do nobre, sem nada que o shell (bash ou zsh) expanda ou execute. É o que a
// pausa por limite deixa passar: o agente registra o handoff, mas
// "nobre status && make test" não fura o bloqueio.
//
// É uma lista branca, porque bash e zsh têm expansões demais para recusar uma
// a uma ($'…', comentários, ${(e)…}, glob qualifiers, =(…)):
//   - fora de aspas, só letras, dígitos, espaço, _ . / : , @ % + = - e bytes
//     não ASCII (acentos);
//   - entre aspas simples, qualquer coisa;
//   - entre aspas duplas, qualquer coisa menos $, crase e \;
//   - aspas sem fechar recusam.
func plainNobreCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	rest, ok := strings.CutPrefix(cmd, "nobre ")
	if !ok {
		return false
	}
	sub, _, _ := strings.Cut(strings.TrimLeft(rest, " "), " ")
	if !handoffSubcommands[sub] {
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
			switch c {
			case '"':
				state = bare
			case '$', '`', '\\':
				return false
			}
		default:
			switch {
			case c == '\'':
				state = single
			case c == '"':
				state = double
			case !bareSafe(c):
				return false
			}
		}
	}
	return state == bare
}

func bareSafe(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c >= 0x80:
		return true
	}
	return strings.IndexByte(" _./:,@%+=-", c) >= 0
}
