package api

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

type batchItem struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// cleanBatchPath valida um caminho do lote (absoluto, fora da raiz e da home)
// e devolve a mensagem de erro ("" se está certo).
func cleanBatchPath(p string) (string, string) {
	if !filepath.IsAbs(p) {
		return "", "caminho do projeto deve ser absoluto"
	}
	p = filepath.Clean(p)
	if homeOrAncestor(p) {
		return "", msgHomeProject
	}
	return p, ""
}

// batchProjects: POST /api/projects/batch cadastra o principal (se ainda não
// existe) e os filhos de uma vez, tudo ou nada.
func (s *server) batchProjects(w http.ResponseWriter, r *http.Request) {
	var in struct {
		batchItem
		Children []batchItem `json:"children"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	path, msg := cleanBatchPath(in.Path)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	specs := make([]store.ChildSpec, 0, len(in.Children))
	for _, c := range in.Children {
		cp, msg := cleanBatchPath(c.Path)
		if msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = filepath.Base(cp)
		}
		specs = append(specs, store.ChildSpec{Name: name, Path: cp})
	}
	ps, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	parent := domain.Project{Name: strings.TrimSpace(in.Name), Path: path, Kind: "repo"}
	if parent.Name == "" {
		parent.Name = filepath.Base(path)
	}
	created := true
	if reg := projectByPath(ps)[path]; reg != nil {
		parent, created = *reg, false
	} else {
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			writeError(w, http.StatusBadRequest, msgNotDir)
			return
		}
		if !isGitDir(path) {
			parent.Kind = "group"
		}
	}
	p, kids, moved, err := s.st.CreateFamily(parent, specs)
	if err != nil {
		writeProjectError(w, err, "projeto não encontrado")
		return
	}
	// A base do repositório novo e dos filhos de grupo (cada um é um repositório).
	if br := s.branchReader(); br != nil {
		if created && !p.IsGroup() {
			s.detectBase(&p, br)
		}
		if p.IsGroup() {
			for i := range kids {
				if kids[i].BaseBranch == "" {
					s.detectBase(&kids[i], br)
				}
			}
		}
	}
	// A adoção roda depois do commit (resolve o dono de cada sessão pela regra do
	// hook, o que não cabe na transação). Se falhar, o cadastro já valeu: registra
	// e devolve a família criada, com o que foi adotado até ali.
	n, err := s.adoptRootSessions(p)
	if err != nil {
		log.Printf("lote: adoção de sessões de %s parou: %v", p.Path, err)
	}
	moved.Sessions += n
	s.publish("config", "sessions", "cards")
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, struct {
		Project  domain.Project   `json:"project"`
		Children []domain.Project `json:"children"`
		Moved    store.Moved      `json:"moved"`
	}{p, kids, moved})
}

// detectBase grava a base detectada do repositório p (falha só ignora).
func (s *server) detectBase(p *domain.Project, br gitinfo.BranchReader) {
	if base := br.DetectBase(p.Path); base != "" && s.st.SetProjectBase(p.ID, base) == nil {
		p.BaseBranch = base
	}
}

// adoptRootSessions adota as sessões sem projeto (vivas e encerradas) cuja
// pasta fica dentro do principal, ou num worktree ligado fora dela, e cujo
// dono, pela regra do hook, é um membro da família com captura ligada.
// Devolve quantas foram adotadas.
func (s *server) adoptRootSessions(root domain.Project) (int, error) {
	if root.Path == "" {
		return 0, nil
	}
	fam, err := s.st.FamilyIDs(root.ID)
	if err != nil {
		return 0, err
	}
	inFam := map[int64]bool{}
	for _, id := range fam {
		inFam[id] = true
	}
	sessions, err := s.st.UnassignedSessionsUnder(root.Path)
	if err != nil {
		return 0, err
	}
	var wr gitinfo.WorktreeReader
	if g, ok := s.deps.Git.(gitinfo.WorktreeReader); ok {
		// Um Locate por worktree, não por sessão.
		wr = &locateCache{WorktreeReader: g, seen: map[string]gitinfo.Location{}}
		linked, err := s.st.UnassignedLinkedSessions()
		if err != nil {
			return 0, err
		}
		under := map[string]bool{}
		for _, x := range sessions {
			under[x.ID] = true
		}
		for _, x := range linked {
			if !under[x.ID] {
				sessions = append(sessions, x)
			}
		}
	}
	n := 0
	for _, x := range sessions {
		owner, err := s.sessionOwner(wr, x)
		if err != nil {
			return n, err
		}
		// Captura desligada funciona como "ignorar", como no hook.
		if owner == nil || !owner.Capture || !inFam[owner.ID] {
			continue
		}
		if err := s.st.AdoptSession(x.ID, owner.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// locateCache guarda o Locate de cada pasta durante uma adoção.
type locateCache struct {
	gitinfo.WorktreeReader
	seen map[string]gitinfo.Location
}

func (c *locateCache) Locate(dir string) gitinfo.Location {
	if l, ok := c.seen[dir]; ok {
		return l
	}
	l := c.WorktreeReader.Locate(dir)
	c.seen[dir] = l
	return l
}
