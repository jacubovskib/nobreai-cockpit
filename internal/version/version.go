// Package version guarda a versão dos binários, gravada na compilação.
package version

// Version vale "dev" sem ldflags; o Makefile e a release a sobrescrevem com
// -X github.com/nobretech/nobreai-cockpit/internal/version.Version=<v>.
var Version = "dev"
