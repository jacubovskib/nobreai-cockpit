package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/nobretech/nobreai-cockpit/web"
)

const noBuildPage = `<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><title>NobreAI Cockpit</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{margin:0;background:#0E1014;color:#E8EAEE;font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:100vh}
main{max-width:520px;padding:24px}code{background:#15181E;border:1px solid #232730;border-radius:6px;padding:2px 8px}</style></head>
<body><main><h1>Interface web não compilada</h1>
<p>Este binário foi gerado sem a interface. Rode <code>make web</code> e depois <code>make build</code> (ou <code>go build</code>) para embuti-la.</p>
<p>A API continua disponível em <code>/api</code>.</p></main></body></html>`

// distFS devolve a raiz do build embutido.
func distFS() fs.FS {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}

// spaHandler serve os arquivos da SPA; caminhos que não são arquivos caem em
// index.html (rotas do react-router). Sem index.html mostra a página de aviso.
// Caminhos /api/ desconhecidos respondem 404 JSON, nunca HTML.
func spaHandler(fsys fs.FS) http.Handler {
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "rota não encontrada")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "método não permitido", http.StatusMethodNotAllowed)
			return
		}
		if _, err := fs.Stat(fsys, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(noBuildPage))
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" {
			if st, err := fs.Stat(fsys, p); err == nil && !st.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}
