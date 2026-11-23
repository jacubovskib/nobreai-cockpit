package agentcore

import (
	"path/filepath"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Locate consulta o git sobre cwd: branch, HEAD e worktree (este só quando o
// Git sabe ler worktrees).
func (c *Core) Locate(cwd string) store.SessionLocation {
	l := store.SessionLocation{Cwd: cwd}
	if c.deps.Git == nil || cwd == "" {
		return l
	}
	l.Branch, l.GitHead = c.deps.Git.Branch(cwd), c.deps.Git.Head(cwd)
	if wr, ok := c.deps.Git.(gitinfo.WorktreeReader); ok {
		loc := wr.Locate(cwd)
		l.Worktree, l.WorktreeLinked, l.Main = loc.Root, loc.Linked, loc.Main
	}
	return l
}

// moveSession grava o novo diretório da sessão (o agente entrou num worktree,
// por exemplo) com branch, HEAD e worktree de lá. O projeto ligado não muda.
func (c *Core) moveSession(sess domain.Session, cwd string) (domain.Session, error) {
	c.outsideSwitch.Delete(sess.ID) // o card acompanha a mudança de diretório
	l := c.Locate(cwd)
	if err := c.st.UpdateSessionLocation(sess.ID, l); err != nil {
		return sess, err
	}
	c.publish("sessions")
	sess.Cwd, sess.Branch, sess.GitHead, sess.Worktree, sess.WorktreeLinked = l.Cwd, l.Branch, l.GitHead, l.Worktree, l.WorktreeLinked
	c.SyncCardBranch(sess)
	return sess, nil
}

// SyncCardBranch faz o branch do card da sessão acompanhar o da sessão (a
// base do projeto nunca vira branch do card). Falha só vai para o log.
func (c *Core) SyncCardBranch(sess domain.Session) {
	if sess.CardID == nil {
		return
	}
	br, _ := c.deps.Git.(gitinfo.BranchReader) // nil quando o Git não lê branches
	changed, err := board.SyncCardBranch(c.st, br, *sess.CardID, sess.Branch)
	if err != nil {
		c.logf("branch do card da sessão %s: %v", sess.ID, err)
		return
	}
	if changed {
		c.publish("cards")
	}
}

// followBranch percebe troca de branch no mesmo diretório (git switch num
// comando, ou fora do agente): grava o branch atual na sessão e devolve a
// sessão atualizada; com syncCard, o card acompanha. Custa um git rev-parse
// por comando, prompt e fim de turno; falhas só vão para o log.
func (c *Core) followBranch(sess domain.Session, syncCard bool) domain.Session {
	if c.deps.Git == nil || sess.Cwd == "" {
		return sess
	}
	b := c.deps.Git.Branch(sess.Cwd)
	if b == "" {
		return sess
	}
	if b == sess.Branch {
		// A sessão já acompanhou (uma troca vista no prompt ou no fim de
		// turno, que não move o card): agora que o próprio agente trabalha
		// nesse branch, o card vem junto.
		// Só quando o card seguia a sessão: estava no branch de antes da
		// troca, ou sem branch (o card nunca guarda a base). Um nobre branch
		// dado de propósito fica.
		if prev, ok := c.outsideSwitch.Load(sess.ID); ok && syncCard && sess.CardID != nil {
			c.outsideSwitch.Delete(sess.ID)
			if cd, err := c.st.GetCardByID(*sess.CardID); err == nil && (cd.Branch == prev.(string) || cd.Branch == "") {
				c.SyncCardBranch(sess)
			}
		}
		return sess
	}
	l := c.Locate(sess.Cwd)
	// Vale uma leitura só: se a do Locate discordar (falhou ou o branch mudou
	// no meio), nada é gravado; o próximo evento tenta de novo.
	if l.Branch != b {
		return sess
	}
	if err := c.st.UpdateSessionLocation(sess.ID, l); err != nil {
		c.logf("branch da sessão %s: %v", sess.ID, err)
		return sess
	}
	c.publish("sessions")
	prev := sess.Branch
	sess.Branch, sess.GitHead, sess.Worktree, sess.WorktreeLinked = l.Branch, l.GitHead, l.Worktree, l.WorktreeLinked
	if syncCard {
		c.outsideSwitch.Delete(sess.ID)
		c.SyncCardBranch(sess)
	} else {
		// Guarda o branch de antes da primeira troca de fora (A→B→C: vale A).
		c.outsideSwitch.LoadOrStore(sess.ID, prev)
	}
	return sess
}

// projectRoot é a raiz do projeto vista pela sessão: num worktree ligado, o
// diretório equivalente dentro do worktree. Um grupo não é repositório: fica a
// pasta dele, sem git.
func (c *Core) projectRoot(sess domain.Session, pr domain.Project) string {
	root := filepath.Clean(pr.Path)
	if pr.IsGroup() || !sess.WorktreeLinked || sess.Worktree == "" {
		return root
	}
	wr, ok := c.deps.Git.(gitinfo.WorktreeReader)
	if !ok {
		return root
	}
	main, cached := c.repoRoots.Load(root)
	if !cached {
		main = wr.Locate(root).Root
		if main != "" { // falha do git não fica no cache
			c.repoRoots.Store(root, main)
		}
	}
	rel, err := filepath.Rel(main.(string), root)
	if main.(string) == "" || err != nil || strings.HasPrefix(rel, "..") {
		return root
	}
	return filepath.Join(sess.Worktree, rel)
}

// projectRelative devolve o caminho relativo à raiz do projeto da sessão
// quando o arquivo está dentro dela (casa com os arquivos essenciais, que o
// agente registra relativos); fora do projeto, fica absoluto.
func (c *Core) projectRelative(sess domain.Session, path string) (string, error) {
	if sess.ProjectID == nil || !filepath.IsAbs(path) {
		return path, nil
	}
	projects, err := c.st.ListProjects()
	if err != nil {
		return "", err
	}
	for _, pr := range projects {
		if pr.ID != *sess.ProjectID {
			continue
		}
		root := c.projectRoot(sess, pr)
		clean := filepath.Clean(path)
		if rel, err := filepath.Rel(root, clean); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") {
			return rel, nil
		}
	}
	return path, nil
}
