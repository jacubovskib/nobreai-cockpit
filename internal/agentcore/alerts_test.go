package agentcore

import "testing"

func TestFmtTokens(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0", 999: "999", 1000: "1 mil", 22000: "22 mil", 22400: "22 mil", 22600: "23 mil", 820000: "820 mil",
		999600: "1 milhão", 1000000: "1 milhão", 1200000: "1,2 milhão", 1960000: "2 milhões", 2500000: "2,5 milhões",
	} {
		if got := fmtTokens(n); got != want {
			t.Errorf("fmtTokens(%d) = %q, quer %q", n, got, want)
		}
	}
}

func TestContextWarningText(t *testing.T) {
	got := contextWarningText(82, 164000)
	want := "Contexto em 82% da janela (164 mil tokens). Numa pausa natural, registre onde parou com nobre note e use /compact dizendo o que preservar, ou recomece limpo pelo card."
	if got != want {
		t.Fatalf("texto = %q", got)
	}
}
