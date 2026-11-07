// Package launch inicia processos de agente fora do daemon (handoff pela UI).
package launch

// Launcher inicia um processo e o solta do daemon.
type Launcher interface {
	Start(argv []string) error
}

// Spec descreve o processo a iniciar.
type Spec struct {
	Argv []string
}
