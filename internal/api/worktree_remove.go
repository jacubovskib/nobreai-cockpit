package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/resources"
)

const (
	msgRemoveUnavailable = "exclusão de worktrees indisponível neste daemon"
	msgProcsUnknown      = "não consegui verificar os processos agora; confirme para excluir mesmo assim"
	msgGitUnknown        = "não consegui ler o estado do git agora; confirme para excluir mesmo assim"
	msgSubmodules        = "o worktree tem submódulos; confirme para excluir mesmo assim"
	msgNotAWorktree      = "o git não reconhece mais esta pasta como worktree; confira e apague à mão se for o caso"
	msgNeedsConfirm      = "o worktree tem alterações não commitadas ou processos rodando; confirme para excluir mesmo assim"
	msgLiveSession       = "há uma sessão viva neste worktree; encerre-a antes de excluir"
)

type removeRequest struct {
	Project      int64  `json:"project"`
	Path         string `json:"path"`
	Force        bool   `json:"force"`
	DeleteBranch bool   `json:"delete_branch"`
}

// RemoveResult é a resposta de DELETE /api/worktrees. BranchKeptReason diz por
// que o branch ficou quando delete_branch foi pedido e não foi apagado.
type RemoveResult struct {
	Removed          bool   `json:"removed"`
	BranchDeleted    bool   `json:"branch_deleted"`
	BranchKeptReason string `json:"branch_kept_reason"`
}

type removeProc struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// needsConfirm é o corpo do 409 que pede confirmação ao usuário.
type needsConfirm struct {
	Error        string               `json:"error"`
	NeedsConfirm bool                 `json:"needs_confirm"`
	Files        []gitinfo.FileChange `json:"files"`
	TotalFiles   int                  `json:"total_files"`
	Processes    []removeProc         `json:"processes"`
	// ProcessesUnknown: o medidor de recursos não tinha amostra pronta.
	ProcessesUnknown bool `json:"processes_unknown"`
	// GitUnknown: o status do git não pôde ser lido (ou não é confiável).
	GitUnknown bool `json:"git_unknown"`
	// Commits que só o HEAD do worktree alcança (até 20) e o total deles.
	Commits      []gitinfo.Commit `json:"commits"`
	TotalCommits int              `json:"total_commits"`
	Rebase       bool             `json:"rebase_in_progress"`
	// RepoSessions conta sessões vivas em outro lugar do mesmo repositório:
	// um subagente delas pode estar neste worktree sem aparecer no registro.
	RepoSessions int `json:"repo_sessions"`
}

func newNeedsConfirm(msg string) needsConfirm {
	return needsConfirm{Error: msg, NeedsConfirm: true, Files: []gitinfo.FileChange{}, Processes: []removeProc{}, Commits: []gitinfo.Commit{}}
}

// under diz se path é dir ou está dentro dele.
func under(path, dir string) bool {
	dir = strings.TrimRight(dir, "/")
	return path == dir || strings.HasPrefix(path, dir+"/")
}

// removeWorktree: DELETE /api/worktrees. Só apaga um worktree ligado que o
// git lista para o projeto, sem sessão viva e destravado; alterações ou
// processos exigem force. O branch só sai se já estiver integrado na base.
func (s *server) removeWorktree(w http.ResponseWriter, r *http.Request) {
	var req removeRequest
	if !readJSON(w, r, &req) {
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path é obrigatório")
		return
	}
	path := filepath.Clean(req.Path)
	p, err := s.projectByID(req.Project)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if p == nil {
		writeError(w, http.StatusNotFound, "projeto não encontrado")
		return
	}
	// O worktree de um app é o do repositório do pai.
	repoP, err := s.st.RepoProject(*p)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	p = &repoP
	remover, ok1 := s.deps.Git.(gitinfo.WorktreeRemover)
	status, ok2 := s.deps.Git.(gitinfo.StatusReader)
	branches, ok3 := s.deps.Git.(gitinfo.BranchReader)
	lister, ok4 := s.deps.Git.(gitinfo.WorktreeReader)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		writeError(w, http.StatusServiceUnavailable, msgRemoveUnavailable)
		return
	}
	pw, err := s.projectWorktrees(*p)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if pw.Error != "" {
		writeError(w, http.StatusBadGateway, pw.Error)
		return
	}
	idx, mainIdx := -1, -1
	for i, v := range pw.Worktrees {
		if v.Path == path {
			idx = i
		}
		if v.Main {
			mainIdx = i
		}
	}
	if idx < 0 || mainIdx < 0 {
		writeError(w, http.StatusNotFound, msgWorktreeNotFound)
		return
	}
	v := pw.Worktrees[idx]
	repo := pw.Worktrees[mainIdx].Path
	switch {
	case v.Main || v.Bare:
		writeError(w, http.StatusBadRequest, "o checkout principal não pode ser excluído")
		return
	case v.Locked:
		msg := "worktree travado"
		if v.LockReason != "" {
			msg += " (" + v.LockReason + ")"
		}
		writeError(w, http.StatusConflict, msg+"; destrave com git worktree unlock")
		return
	}

	// Apagar o pai apagaria o worktree aninhado (e o que ele tem não commitado).
	for i, x := range pw.Worktrees {
		if i != idx && x.Path != v.Path && under(x.Path, v.Path) {
			writeError(w, http.StatusConflict, "há outro worktree dentro deste ("+x.Path+"); exclua-o antes")
			return
		}
	}

	// Nem a pasta de um projeto cadastrado (de qualquer projeto).
	projects, err := s.st.ListProjects()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	for _, o := range projects {
		if o.Path != "" && under(filepath.Clean(o.Path), v.Path) {
			writeError(w, http.StatusConflict, "a pasta de um projeto cadastrado está aqui ("+o.Name+"); remova o projeto antes")
			return
		}
	}

	repoSessions, inThis, err := s.liveSessionsFor(pw.Worktrees, idx, v.Path)
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	if inThis {
		writeError(w, http.StatusConflict, msgLiveSession)
		return
	}

	// Prunable só é "pasta sumiu" se a pasta sumiu mesmo. Com ela lá (sem
	// .git), o status subiria para o repositório de cima: não é confiável.
	_, statErr := os.Stat(v.Path)
	missing := v.Prunable && statErr != nil
	strayFolder := v.Prunable && !missing

	if !req.Force && !missing {
		nc := newNeedsConfirm(msgNeedsConfirm)
		if strayFolder {
			nc.GitUnknown = true
		} else if in, err := status.FullStatus(v.Path); err == nil {
			nc.Files, nc.TotalFiles = nonNil(in.Files), in.TotalFiles
		} else {
			nc.GitUnknown = true
		}
		if s.deps.Resources != nil {
			procs, ready := s.deps.Resources.Snapshot()
			if !ready || procs == nil {
				nc.ProcessesUnknown = true
			}
			paths := make([]string, len(pw.Worktrees))
			for i, x := range pw.Worktrees {
				paths[i] = x.Path
			}
			for _, pr := range resources.Attribute(procs, paths)[strings.TrimRight(v.Path, "/")].All {
				nc.Processes = append(nc.Processes, removeProc{PID: pr.PID, Name: pr.Comm})
			}
		}
		if orph, ok := s.deps.Git.(gitinfo.OrphanReader); ok && !strayFolder {
			if c, total, rebase, err := orph.OrphanCommits(v.Path); err == nil {
				nc.Commits, nc.TotalCommits, nc.Rebase = c, total, rebase
			} else {
				nc.GitUnknown = true
			}
		}
		nc.RepoSessions = repoSessions
		known := nc.TotalFiles > 0 || len(nc.Processes) > 0 || nc.TotalCommits > 0 || nc.Rebase || nc.RepoSessions > 0
		if known || nc.GitUnknown || nc.ProcessesUnknown {
			switch {
			case known:
			case nc.GitUnknown:
				nc.Error = msgGitUnknown
			default:
				nc.Error = msgProcsUnknown
			}
			writeJSON(w, http.StatusConflict, nc)
			return
		}
	}

	// De novo logo antes de apagar: o status acima pode ter demorado, e uma
	// sessão que começou nesse meio-tempo não pode perder a pasta.
	if _, inThis, err := s.liveSessionsFor(pw.Worktrees, idx, v.Path); err != nil {
		writeStoreError(w, err, "")
		return
	} else if inThis {
		writeError(w, http.StatusConflict, msgLiveSession)
		return
	}

	if missing {
		// Remove só este registro; o prune (que limpa todos) fica de reserva.
		if err = remover.RemoveWorktree(repo, v.Path, false); err != nil {
			err = remover.PruneWorktrees(repo)
		}
	} else {
		err = remover.RemoveWorktree(repo, v.Path, req.Force)
	}
	switch {
	case errors.Is(err, gitinfo.ErrSubmodules):
		writeJSON(w, http.StatusConflict, newNeedsConfirm(msgSubmodules))
		return
	case err != nil && strayFolder:
		writeError(w, http.StatusConflict, msgNotAWorktree)
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "não consegui excluir o worktree: "+err.Error())
		return
	}

	res := RemoveResult{Removed: true}
	if req.DeleteBranch && (v.Branch == "" || v.Detached) {
		res.BranchKeptReason = "o worktree não tem branch (HEAD destacado)"
	} else if req.DeleteBranch {
		res.BranchDeleted, res.BranchKeptReason = deleteIntegratedBranch(remover, branches, lister, *p, repo, v.Branch)
	}
	s.publish("sessions")
	if res.BranchDeleted {
		s.publish("cards")
	}
	writeJSON(w, http.StatusOK, res)
}

// deleteIntegratedBranch apaga branch só se a ponta dele já estiver na base
// do projeto (local ou origin/<base>) e nenhum outro worktree o usar; apaga
// com compare-and-delete na ponta verificada. Devolve se apagou e, se não,
// o motivo em pt-BR.
func deleteIntegratedBranch(remover gitinfo.WorktreeRemover, br gitinfo.BranchReader, wr gitinfo.WorktreeReader, p domain.Project, repo, branch string) (bool, string) {
	base := board.ProjectBase(br, p)
	switch {
	case base == "":
		return false, "a base do projeto não foi encontrada"
	case branch == base:
		return false, "é a base do projeto"
	}
	tip, ok := br.BranchTip(repo, branch)
	if !ok {
		return false, "o branch não foi encontrado"
	}
	if !br.IsAncestor(repo, tip, base) && !br.IsAncestor(repo, tip, "origin/"+base) {
		return false, "não está integrado em " + base
	}
	// Lista de novo, já sem o worktree removido.
	list, err := wr.Worktrees(repo)
	if err != nil {
		return false, "não consegui apagar o branch: não consegui listar os worktrees"
	}
	for _, x := range list {
		if x.Branch == branch && !x.Detached {
			return false, "o branch está em uso em outro worktree (" + x.Path + ")"
		}
	}
	if err := remover.DeleteBranchAt(repo, branch, tip); err != nil {
		return false, "não consegui apagar o branch: " + err.Error()
	}
	return true, ""
}

// liveSessionsFor olha as sessões vivas de qualquer época e de qualquer
// projeto (estar viva é uma propriedade do worktree). inThis: alguma está no
// worktree idx (ou com o cwd dentro dele: um clone independente dentro do
// alvo grava outra raiz). others conta as que estão em outro worktree do
// mesmo repositório.
func (s *server) liveSessionsFor(wts []WorktreeView, idx int, path string) (others int, inThis bool, err error) {
	all, err := s.st.LiveSessions()
	if err != nil {
		return 0, false, err
	}
	for _, x := range all {
		if !x.Live() {
			continue
		}
		switch i := worktreeOf(wts, x); {
		case i == idx || under(x.Cwd, path):
			return 0, true, nil
		case i >= 0:
			others++
		}
	}
	return others, false, nil
}
