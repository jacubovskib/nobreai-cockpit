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
		{`nobre note "custa $HOME"`, true},
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
		{`nobre note "escapa \" e fica"`, true},
		{`nobre note sem\;aspas`, true},
	}
	for _, c := range cases {
		if got := plainNobreCommand(c.cmd); got != c.want {
			t.Errorf("plainNobreCommand(%q) = %v, quer %v", c.cmd, got, c.want)
		}
	}
}
