package claude

import "testing"

func TestPlainNobreCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{`nobre note "callback pronto"`, true},
		{`  nobre status  `, true},
		{`nobre note 'usa ; e | e && dentro de aspas simples'`, true},
		{`nobre note "texto com ; | & > < dentro de aspas duplas"`, true},
		{`nobre decide "PKCE" --why "o app é público"`, true},
		{`nobre note "custa $HOME"`, false},
		{`/home/u/.local/bin/nobre status`, false},
		{`nobre`, false},
		{`nobrex status`, false},
		{`nobre status && rm -rf build`, false},
		{`nobre status; make test`, false},
		{`nobre status | sh`, false},
		{`nobre status & make test`, false},
		{`nobre status > /tmp/x`, false},
		{`nobre prompt save --name x --tag y < corpo.txt`, false},
		{"nobre status\nmake test", false},
		{"nobre note \"$(make test)\"", false},
		{"nobre note \"`make test`\"", false},
		{"nobre note `make test`", false},
		{`nobre note $(make test)`, false},
		{`nobre note "aspas sem fechar`, false},
		{`nobre note 'aspas sem fechar`, false},
		{`nobre note "escapa \" e segue" ; make test`, false},
		{`nobre note "escapa \" e fica"`, false},
		{`nobre note sem\;aspas`, false},
	}
	for _, c := range cases {
		if got := plainNobreCommand(c.cmd); got != c.want {
			t.Errorf("plainNobreCommand(%q) = %v, quer %v", c.cmd, got, c.want)
		}
	}
}

// Bypasses achados na revisão (testados em bash e zsh) e subcomandos que
// executam outra coisa.
func TestPlainNobreCommandReviewBypasses(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{`nobre note $'a\' ' ; make test #'`, false},
		{"nobre note #'\nmake test\n#'", false},
		{`nobre note ${(e):-'$(make test)'}`, false},
		{`nobre note *(e:'make test':)`, false},
		{`nobre note =(make test)`, false},
		{`nobre note ~/x`, false},
		{`nobre note a\ b`, false},
		{`nobre note "a\"b"`, false},
		{`nobre run make test`, false},
		{`nobre run sh -c 'echo x'`, false},
		{`nobre setup --remover`, false},
		{`nobre hook claude Stop`, false},
		{`nobre ui`, false},
		{`nobre statusx`, false},
		{`nobre note onde parei: ação concluída, falta o teste`, true},
		{`nobre todo done 3`, true},
		{`nobre file internal/x.go --why "motivo" --lines L12-96`, true},
		{`nobre answer 4 'cookie httpOnly'`, true},
		{`nobre block NAC-4 --why="precisa do merge"`, true},
		{`nobre done`, true},
		{`nobre status`, true},
	}
	for _, c := range cases {
		if got := plainNobreCommand(c.cmd); got != c.want {
			t.Errorf("plainNobreCommand(%q) = %v, quer %v", c.cmd, got, c.want)
		}
	}
}
