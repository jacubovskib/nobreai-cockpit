package domain

import (
	"path"
	"strings"
	"time"
)

// TaskStatus é o estado de uma tarefa do plano.
type TaskStatus string

const (
	TaskTodo   TaskStatus = "a_fazer"
	TaskDoing  TaskStatus = "em_andamento"
	TaskReview TaskStatus = "em_revisao"
	TaskDone   TaskStatus = "concluida"
)

// ParseTaskStatus aceita os valores gravados e as grafias do CLI (a-fazer, andamento, revisao).
func ParseTaskStatus(s string) (TaskStatus, bool) {
	switch strings.ReplaceAll(strings.TrimSpace(s), "-", "_") {
	case "a_fazer":
		return TaskTodo, true
	case "andamento", "em_andamento":
		return TaskDoing, true
	case "revisao", "em_revisao":
		return TaskReview, true
	case "concluida":
		return TaskDone, true
	}
	return "", false
}

// Label é o rótulo exibido ao usuário.
func (s TaskStatus) Label() string {
	switch s {
	case TaskTodo:
		return "a fazer"
	case TaskDoing:
		return "em andamento"
	case TaskReview:
		return "em revisão"
	case TaskDone:
		return "concluída"
	}
	return string(s)
}

// PlanTask é uma tarefa do plano da entrega.
type PlanTask struct {
	N         int        `json:"n"`
	Title     string     `json:"title"`
	Status    TaskStatus `json:"status"`
	SessionID string     `json:"session_id"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Plan é o plano registrado no card: o arquivo e as tarefas, em ordem de n.
type Plan struct {
	Path  string     `json:"path"`
	Tasks []PlanTask `json:"tasks"`
}

// Open conta as tarefas não concluídas (0 com p nil).
func (p *Plan) Open() int {
	if p == nil {
		return 0
	}
	n := 0
	for _, t := range p.Tasks {
		if t.Status != TaskDone {
			n++
		}
	}
	return n
}

// DefaultPlanPatterns são os padrões de arquivo de plano iniciais.
func DefaultPlanPatterns() []string {
	return []string{"docs/plan*.md", "docs/superpowers/plans/*.md", ".kiro/specs/*/tasks.md"}
}

// MatchPlan diz se o caminho rel, relativo ao projeto e com barras, casa com algum padrão.
// Caminho absoluto nunca casa.
func MatchPlan(patterns []string, rel string) bool {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return false
	}
	for _, p := range patterns {
		if ok, err := path.Match(p, rel); err == nil && ok {
			return true
		}
	}
	return false
}

// cmdSplitter troca os separadores de comando do shell por quebra de linha.
var cmdSplitter = strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")

// CommandParts separa a linha de comando em &&, ||, ; e |, cada parte em
// tokens separados por espaço (as aspas não são tiradas).
func CommandParts(cmd string) [][]string {
	var out [][]string
	for _, part := range strings.Split(cmdSplitter.Replace(cmd), "\n") {
		if f := strings.Fields(part); len(f) > 0 {
			out = append(out, f)
		}
	}
	return out
}

// IsGitCommit diz se alguma parte da linha de comando roda git commit: depois
// do token git, pula as opções (-C e -c levam o argumento seguinte) e o
// próximo token tem de ser commit.
func IsGitCommit(cmd string) bool {
	for _, f := range CommandParts(cmd) {
		for i, tok := range f {
			if tok != "git" {
				continue
			}
			j := i + 1
			for j < len(f) && strings.HasPrefix(f[j], "-") {
				if f[j] == "-C" || f[j] == "-c" {
					j++
				}
				j++
			}
			if j < len(f) && f[j] == "commit" {
				return true
			}
		}
	}
	return false
}
