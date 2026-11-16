package api

import (
	"net/http"
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
	msgNeedsConfirm      = "o worktree tem alterações não commitadas ou processos rodando; confirme para excluir mesmo assim"
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
	remover, ok1 := s.deps.Git.(gitinfo.WorktreeRemover)
	inspector, ok2 := s.deps.Git.(gitinfo.WorktreeInspector)
	branches, ok3 := s.deps.Git.(gitinfo.BranchReader)
	if !ok1 || !ok2 || !ok3 {
		writeError(w, http.StatusServiceUnavailable, msgRemoveUnavailable)
		return
	}
	pw, err := s.projectWorktrees(*p)
	if err != nil {
		writeStoreError(w, err, "")
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
		if i != idx && strings.HasPrefix(x.Path, strings.TrimRight(v.Path, "/")+"/") {
			writeError(w, http.StatusConflict, "há outro worktree dentro deste ("+x.Path+"); exclua-o antes")
			return
		}
	}

	// Sessão viva de qualquer época e de qualquer projeto: estar viva é uma
	// propriedade do worktree.
	all, err := s.st.LiveSessions()
	if err != nil {
		writeStoreError(w, err, "")
		return
	}
	for _, x := range all {
		if x.Live() && worktreeOf(pw.Worktrees, x) == idx {
			writeError(w, http.StatusConflict, "há uma sessão viva neste worktree; encerre-a antes de excluir")
			return
		}
	}

	if !req.Force && !v.Prunable {
		nc := needsConfirm{Error: msgNeedsConfirm, NeedsConfirm: true, Files: []gitinfo.FileChange{}, Processes: []removeProc{}}
		if in, err := inspector.Inspect(v.Path, "", "", 0); err == nil {
			nc.Files, nc.TotalFiles = nonNil(in.Files), in.TotalFiles
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
		if nc.TotalFiles > 0 || len(nc.Processes) > 0 || nc.ProcessesUnknown {
			if nc.TotalFiles == 0 && len(nc.Processes) == 0 {
				nc.Error = msgProcsUnknown
			}
			writeJSON(w, http.StatusConflict, nc)
			return
		}
	}

	if v.Prunable {
		err = remover.PruneWorktrees(repo)
	} else {
		err = remover.RemoveWorktree(repo, v.Path, req.Force)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "não consegui excluir o worktree: "+err.Error())
		return
	}

	res := RemoveResult{Removed: true}
	if req.DeleteBranch && (v.Branch == "" || v.Detached) {
		res.BranchKeptReason = "o worktree não tem branch (HEAD destacado)"
	} else if req.DeleteBranch {
		res.BranchDeleted, res.BranchKeptReason = deleteIntegratedBranch(remover, branches, *p, repo, v.Branch)
	}
	s.publish("sessions")
	writeJSON(w, http.StatusOK, res)
}

// deleteIntegratedBranch apaga branch só se a ponta dele já estiver na base
// do projeto; devolve se apagou e, se não, o motivo.
func deleteIntegratedBranch(remover gitinfo.WorktreeRemover, br gitinfo.BranchReader, p domain.Project, repo, branch string) (bool, string) {
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
	if err := remover.DeleteBranch(repo, branch); err != nil {
		return false, err.Error()
	}
	return true, ""
}
