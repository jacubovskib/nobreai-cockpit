// Package web embute a interface web compilada (web/dist) no binário do nobred.
package web

import "embed"

// Dist contém o build da SPA em dist/. Num clone sem build só existe
// dist/.gitkeep; a API então serve uma página orientando a rodar `make web`.
//
//go:embed all:dist
var Dist embed.FS
