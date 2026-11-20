package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/memory"
)

const (
	maxMemoryRule = 300
	maxMemoryDo   = 400
	// maxCandidateText é o teto de regra e ação de uma candidata (importação
	// e cards): texto acima de 300/400 fica para o usuário editar antes de aprovar.
	maxCandidateText = 4000
	// autoDecisionWhy marca as decisões que o próprio Cockpit registra (internal/board).
	autoDecisionWhy = "detectado pelo Cockpit"
)

// ErrMemory é a base dos erros de validação de memória.
var ErrMemory = errors.New("memória inválida")

func memoryErr(msg string) error { return fmt.Errorf("%w: %s", ErrMemory, msg) }

const memoryCols = `id, project_id, category, rule, do_text, status, source, source_ref, position, needs_review, created_at, updated_at`

func scanMemory(sc interface{ Scan(...any) error }) (domain.Memory, error) {
	var m domain.Memory
	var created, updated string
	if err := sc.Scan(&m.ID, &m.ProjectID, &m.Category, &m.Rule, &m.Do, &m.Status, &m.Source,
		&m.SourceRef, &m.Position, &m.NeedsReview, &created, &updated); err != nil {
		return m, err
	}
	var err error
	if m.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return m, err
	}
	m.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return m, err
}

// cleanMemory limpa e valida regra, ação, categoria, status e origem.
func cleanMemory(m domain.Memory) (domain.Memory, error) {
	m.Rule, _ = memory.CleanText(m.Rule, 0)
	m.Do, _ = memory.CleanText(m.Do, 0)
	if m.Status == "" {
		m.Status = "ativa"
	}
	if m.Rule == "" {
		return m, memoryErr("a regra não pode ser vazia")
	}
	if !memory.ValidCategory(m.Category) {
		return m, memoryErr("categoria desconhecida")
	}
	ruleLen, doLen := utf8.RuneCountInString(m.Rule), utf8.RuneCountInString(m.Do)
	over := ruleLen > maxMemoryRule || doLen > maxMemoryDo
	if m.Status == "ativa" {
		if ruleLen > maxMemoryRule {
			return m, memoryErr("regra acima de 300 caracteres: edite antes de aprovar")
		}
		if doLen > maxMemoryDo {
			return m, memoryErr("ação acima de 400 caracteres: edite antes de aprovar")
		}
		if m.Do == "" {
			return m, memoryErr("falta o faça assim: edite antes de aprovar")
		}
	} else if ruleLen > maxCandidateText || doLen > maxCandidateText {
		return m, memoryErr("texto acima de 4000 caracteres")
	}
	if over {
		m.NeedsReview = true // só chega aqui fora de "ativa"
	}
	if m.Source == "" {
		m.Source = "usuario"
	}
	switch m.Status {
	case "ativa", "candidata", "arquivada", "descartada":
	default:
		return m, memoryErr("status desconhecido")
	}
	switch m.Source {
	case "agente", "usuario", "napkin", "card":
	default:
		return m, memoryErr("origem desconhecida")
	}
	return m, nil
}

// nextMemoryPosition é a posição logo após a última do escopo e categoria.
func nextMemoryPosition(q querier, projectID *int64, category string) (int, error) {
	var pos int
	err := q.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM memories
		WHERE project_id IS ? AND category=?`, projectID, category).Scan(&pos)
	return pos, err
}

func insertMemoryTx(q querier, m domain.Memory) (domain.Memory, error) {
	m, err := cleanMemory(m)
	if err != nil {
		return m, err
	}
	if m.Position, err = nextMemoryPosition(q, m.ProjectID, m.Category); err != nil {
		return m, err
	}
	now := time.Now().UTC()
	// CreatedAt preenchido (data do item do napkin) é mantido.
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.CreatedAt, m.UpdatedAt = m.CreatedAt.UTC(), now
	// origin_project_id: o escopo de nascimento, que não muda depois.
	res, err := q.Exec(`INSERT INTO memories(project_id, origin_project_id, category, rule, rule_norm, origin_norm, do_text, status, source, source_ref, position, needs_review, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ProjectID, m.ProjectID, m.Category, m.Rule, memory.Normalize(m.Rule), memory.Normalize(m.Rule), m.Do, m.Status, m.Source, m.SourceRef,
		m.Position, m.NeedsReview, fmtTime(m.CreatedAt), fmtTime(now))
	if err != nil {
		return m, fmt.Errorf("criar memória: %w", err)
	}
	m.ID, err = res.LastInsertId()
	return m, err
}

// CreateMemory limpa e valida a memória e a põe no fim do escopo e categoria.
// Ativa respeita 300/400 caracteres e exige ação; candidata aceita até 4000 e marca
// needs_review quando passa dos limites. Status vazio vira "ativa" e origem vazia vira "usuario".
func (s *Store) CreateMemory(m domain.Memory) (domain.Memory, error) {
	return insertMemoryTx(s.DB, m)
}

// UpdateMemory altera regra, ação, categoria, escopo, status e needs_review.
// Mudar de escopo ou categoria, ou passar a ativa (aprovar, restaurar), leva a
// memória para o fim do grupo. A origem (origin_norm, origin_project_id) não muda.
func (s *Store) UpdateMemory(m domain.Memory) (domain.Memory, error) {
	m, err := cleanMemory(m)
	if err != nil {
		return m, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	var curProject *int64
	var curCat, created, curRule, curDo, curStatus string
	err = tx.QueryRow(`SELECT project_id, category, source, source_ref, position, created_at, rule, do_text, status FROM memories WHERE id=?`, m.ID).
		Scan(&curProject, &curCat, &m.Source, &m.SourceRef, &m.Position, &created, &curRule, &curDo, &curStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	// Texto editado que agora cabe nos limites e tem ação: a revisão está feita.
	// Sem ação o motivo de revisão continua.
	nowOver := utf8.RuneCountInString(m.Rule) > maxMemoryRule || utf8.RuneCountInString(m.Do) > maxMemoryDo
	if (m.Rule != curRule || m.Do != curDo) && !nowOver && m.Do != "" {
		m.NeedsReview = false
	}
	sameScope := (curProject == nil && m.ProjectID == nil) ||
		(curProject != nil && m.ProjectID != nil && *curProject == *m.ProjectID)
	becomesActive := curStatus != "ativa" && m.Status == "ativa"
	if !sameScope || curCat != m.Category || becomesActive {
		if m.Position, err = nextMemoryPosition(tx, m.ProjectID, m.Category); err != nil {
			return m, err
		}
	}
	if m.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return m, err
	}
	m.UpdatedAt = time.Now().UTC()
	_, err = tx.Exec(`UPDATE memories SET project_id=?, category=?, rule=?, rule_norm=?, do_text=?, status=?, position=?, needs_review=?, updated_at=? WHERE id=?`,
		m.ProjectID, m.Category, m.Rule, memory.Normalize(m.Rule), m.Do, m.Status, m.Position, m.NeedsReview, fmtTime(m.UpdatedAt), m.ID)
	if err != nil {
		return m, err
	}
	return m, tx.Commit()
}

// DiscardArchivedMemory é o "Excluir" da página (rota DELETE): a memória
// arquivada vira descartada, um registro escondido que segue barrando a
// reimportação da mesma regra. As de outro status dão erro.
func (s *Store) DiscardArchivedMemory(id int64) error {
	var status string
	err := s.DB.QueryRow(`SELECT status FROM memories WHERE id=?`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "arquivada" {
		return memoryErr("só memórias arquivadas podem ser excluídas")
	}
	res, err := s.DB.Exec(`UPDATE memories SET status='descartada', updated_at=? WHERE id=? AND status='arquivada'`,
		fmtTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// MoveMemory sobe (dir -1) ou desce (dir +1) a memória dentro do grupo
// (escopo, categoria, ativas). No limite do grupo não faz nada.
func (s *Store) MoveMemory(id int64, dir int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var projectID *int64
	var category, status string
	err = tx.QueryRow(`SELECT project_id, category, status FROM memories WHERE id=?`, id).Scan(&projectID, &category, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != "ativa" || (dir != -1 && dir != 1) {
		return nil
	}
	rows, err := tx.Query(`SELECT id FROM memories WHERE project_id IS ? AND category=? AND status='ativa'
		ORDER BY position, id`, projectID, category)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var x int64
		if err := rows.Scan(&x); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, x)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	at := -1
	for i, x := range ids {
		if x == id {
			at = i
		}
	}
	to := at + dir
	if at < 0 || to < 0 || to >= len(ids) {
		return nil
	}
	ids[at], ids[to] = ids[to], ids[at]
	// Regrava posições 0..n-1 (também desfaz empates).
	for i, x := range ids {
		if _, err := tx.Exec(`UPDATE memories SET position=? WHERE id=?`, i, x); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MemoryFilter escolhe quais memórias listar.
type MemoryFilter struct {
	ProjectIDs []int64 // escopos incluídos (vazio = todos)
	Global     bool    // inclui project_id NULL
	Status     string  // "" = todos menos descartada
	Term       string  // busca em regra/ação (normalizada)
}

// ListMemories lista por escopo, categoria, posição e id. Sem ProjectIDs nem
// Global, devolve todos os escopos. O resultado nunca é nil.
func (s *Store) ListMemories(f MemoryFilter) ([]domain.Memory, error) {
	var conds []string
	var args []any
	if len(f.ProjectIDs) > 0 || f.Global {
		var parts []string
		if f.Global {
			parts = append(parts, "project_id IS NULL")
		}
		if len(f.ProjectIDs) > 0 {
			ph := strings.TrimSuffix(strings.Repeat("?,", len(f.ProjectIDs)), ",")
			parts = append(parts, "project_id IN ("+ph+")")
			for _, id := range f.ProjectIDs {
				args = append(args, id)
			}
		}
		conds = append(conds, "("+strings.Join(parts, " OR ")+")")
	}
	if f.Status != "" {
		conds = append(conds, "status=?")
		args = append(args, f.Status)
	} else {
		conds = append(conds, "status<>'descartada'")
	}
	q := `SELECT ` + memoryCols + ` FROM memories WHERE ` + strings.Join(conds, " AND ") +
		` ORDER BY project_id IS NULL, project_id, category, position, id`
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	term := memory.Normalize(f.Term)
	out := []domain.Memory{}
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		if term != "" && !strings.Contains(memory.Normalize(m.Rule), term) && !strings.Contains(memory.Normalize(m.Do), term) {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func memoryExistsTx(q querier, projectID *int64, rule string) (bool, error) {
	var ok bool
	norm := memory.Normalize(rule)
	err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM memories WHERE project_id IS ? AND (rule_norm=? OR origin_norm=?))`,
		projectID, norm, norm).Scan(&ok)
	return ok, err
}

// MemoryExists diz se já há memória com a mesma regra normalizada no escopo,
// em qualquer status (inclusive descartada).
func (s *Store) MemoryExists(projectID *int64, rule string) (bool, error) {
	return memoryExistsTx(s.DB, projectID, rule)
}

// candidateExistsTx diz se uma candidata vinda dos projetos from (um só para
// candidata local; os do grupo para global) já existe: regra ou origem
// normalizada igual, em qualquer status, numa global, num dos projetos ou
// nascida num deles. Assim mudar de escopo ou descartar não faz a regra voltar.
func candidateExistsTx(q querier, rule string, from []int64) (bool, error) {
	norm := memory.Normalize(rule)
	cond := "project_id IS NULL"
	args := []any{norm, norm}
	if len(from) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(from)), ",")
		cond += " OR project_id IN (" + ph + ") OR origin_project_id IN (" + ph + ")"
		for range 2 {
			for _, id := range from {
				args = append(args, id)
			}
		}
	}
	var ok bool
	err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM memories WHERE (rule_norm=? OR origin_norm=?) AND (`+cond+`))`, args...).Scan(&ok)
	return ok, err
}

// CardMemoryCandidates cria uma memória candidata (categoria dominio, ação =
// porquê) para cada decisão do card, menos as automáticas do Cockpit e as que
// já existem para o projeto do card (candidateExistsTx). Devolve quantas criou.
func (s *Store) CardMemoryCandidates(cardID int64) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var key string
	var projectID int64
	err = tx.QueryRow(`SELECT key, project_id FROM cards WHERE id=?`, cardID).Scan(&key, &projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(`SELECT what, why FROM decisions WHERE card_id=? AND why<>? ORDER BY id`, cardID, autoDecisionWhy)
	if err != nil {
		return 0, err
	}
	type dec struct{ what, why string }
	var decs []dec
	for rows.Next() {
		var d dec
		if err := rows.Scan(&d.what, &d.why); err != nil {
			rows.Close()
			return 0, err
		}
		decs = append(decs, d)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	n := 0
	for _, d := range decs {
		rule, _ := memory.CleanText(d.what, maxCandidateText)
		do, _ := memory.CleanText(d.why, maxCandidateText)
		if rule == "" {
			continue
		}
		exists, err := candidateExistsTx(tx, rule, []int64{projectID})
		if err != nil {
			return 0, err
		}
		if exists {
			continue
		}
		_, err = insertMemoryTx(tx, domain.Memory{
			ProjectID: &projectID, Category: "dominio", Rule: rule, Do: do, Status: "candidata",
			Source: "card", SourceRef: key,
		})
		if err != nil {
			return 0, err
		}
		n++
	}
	return n, tx.Commit()
}

// MemoriesForSession devolve as memórias ativas que a sessão recebe: as do
// projeto efetivo quando ele é app, as da raiz da família e as globais. O
// projeto efetivo é o do card da sessão, senão o da sessão; sem nenhum, tudo
// volta vazio (nunca nil).
func (s *Store) MemoriesForSession(sess domain.Session) (app, root, global []domain.Memory, err error) {
	app, root, global = []domain.Memory{}, []domain.Memory{}, []domain.Memory{}
	p0, err := s.EffectiveProject(sess)
	if err != nil {
		return nil, nil, nil, err
	}
	var eff *int64
	if p0 != nil {
		eff = &p0.ID
	}
	list := func(pid *int64) ([]domain.Memory, error) {
		rows, err := s.DB.Query(`SELECT `+memoryCols+` FROM memories WHERE project_id IS ? AND status='ativa'
			ORDER BY position, id`, pid)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []domain.Memory{}
		for rows.Next() {
			m, err := scanMemory(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, m)
		}
		return out, rows.Err()
	}
	// Sem projeto a sessão ainda recebe as globais.
	if eff != nil {
		p, err := getProjectTx(s.DB, *eff)
		if err != nil {
			return nil, nil, nil, err
		}
		if p.ParentID != nil {
			if app, err = list(&p.ID); err != nil {
				return nil, nil, nil, err
			}
		}
		rootID := RootID(p)
		if root, err = list(&rootID); err != nil {
			return nil, nil, nil, err
		}
	}
	if global, err = list(nil); err != nil {
		return nil, nil, nil, err
	}
	return app, root, global, nil
}

// RootID é o id da raiz da família do projeto (o pai, ou o próprio se já for raiz).
func RootID(p domain.Project) int64 {
	if p.ParentID != nil {
		return *p.ParentID
	}
	return p.ID
}

// EffectiveProject é o projeto do card da sessão, senão o da sessão (nil = nenhum).
func (s *Store) EffectiveProject(sess domain.Session) (*domain.Project, error) {
	var eff *int64
	if sess.CardID != nil {
		var pid int64
		err := s.DB.QueryRow(`SELECT project_id FROM cards WHERE id=?`, *sess.CardID).Scan(&pid)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return nil, err
		default:
			eff = &pid
		}
	}
	if eff == nil {
		eff = sess.ProjectID
	}
	if eff == nil {
		return nil, nil
	}
	p, err := getProjectTx(s.DB, *eff)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ImportItem é uma candidata da importação e os projetos de onde ela veio
// (o próprio, para local; os do grupo, para global).
type ImportItem struct {
	Memory domain.Memory
	From   []int64
}

// ImportMemories cria as candidatas numa transação só: as que já existem para
// os projetos de origem (candidateExistsTx) são puladas. Um erro desfaz tudo.
// Devolve quantas criou e quantas pulou.
func (s *Store) ImportMemories(items []ImportItem) (created, skipped int, err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	for _, it := range items {
		exists, err := candidateExistsTx(tx, it.Memory.Rule, it.From)
		if err != nil {
			return 0, 0, err
		}
		if exists {
			skipped++
			continue
		}
		if _, err := insertMemoryTx(tx, it.Memory); err != nil {
			return 0, 0, err
		}
		created++
	}
	return created, skipped, tx.Commit()
}

// GetMemory devolve a memória pelo id (qualquer status).
func (s *Store) GetMemory(id int64) (domain.Memory, error) {
	m, err := scanMemory(s.DB.QueryRow(`SELECT `+memoryCols+` FROM memories WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}
