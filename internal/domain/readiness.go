package domain

import (
	"strings"
	"time"
)

// Content reúne o conteúdo estruturado de um card.
type Content struct {
	Decisions []Decision      `json:"decisions"`
	Files     []EssentialFile `json:"files"`
	Todos     []Todo          `json:"todos"`
	Questions []Question      `json:"questions"`
}

// Settings são as preferências do usuário, persistidas na tabela settings.
type Settings struct {
	Rules                 map[string]bool `json:"rules"`
	WhereStoppedTTLMin    int             `json:"where_stopped_ttl_min"`
	GraceMin              int             `json:"grace_min"`
	ReminderEditThreshold int             `json:"reminder_edit_threshold"`
	LaunchMode            string          `json:"launch_mode"`
	TerminalCmd           []string        `json:"terminal_cmd"`
	Currency              string          `json:"currency"`
	BRLRate               float64         `json:"brl_rate"`
	IncompleteAction      string          `json:"incomplete_action"` // "ask_agent" ou "mark"
	// PlanWarnPct é o percentual dos limites do plano a partir do qual a sala de controle avisa.
	PlanWarnPct int `json:"plan_warn_pct"`
	// Demo marca o banco de demonstração (nobred --seed-demo): a UI mostra a faixa "Dados de demonstração".
	Demo bool `json:"demo"`
	// AgentCreatesCards: o agente cria card por conta própria; desligado, só quando o usuário pede.
	AgentCreatesCards bool `json:"agent_creates_cards"`
	// InjectPausedCards: o SessionStart lista os cards pausados do repositório.
	InjectPausedCards bool `json:"inject_paused_cards"`
	// MemoryBudgetTokens é o teto de tokens do bloco de memórias injetado na sessão (0 desliga).
	MemoryBudgetTokens int `json:"memory_budget_tokens"`
}

// DefaultSettings devolve os padrões do piloto.
func DefaultSettings() Settings {
	return Settings{
		Rules: map[string]bool{
			"goal": true, "where_stopped": true, "essential_file": true,
			"todo_open": true, "how_to_test": true, "no_open_questions": false,
		},
		WhereStoppedTTLMin:    15,
		GraceMin:              15,
		ReminderEditThreshold: 3,
		LaunchMode:            "terminal",
		TerminalCmd:           []string{"x-terminal-emulator", "-e"},
		Currency:              "USD",
		IncompleteAction:      "ask_agent",
		PlanWarnPct:           80,
		AgentCreatesCards:     true,
		InjectPausedCards:     true,
		MemoryBudgetTokens:    1500,
	}
}

type ReadinessItem struct {
	Rule  string `json:"rule"`
	Label string `json:"label"`
	OK    bool   `json:"ok"`
}

type ReadinessResult struct {
	Items []ReadinessItem `json:"items"`
	Met   int             `json:"met"`
	Total int             `json:"total"`
	Ready bool            `json:"ready"`
}

var readinessRules = []struct{ key, label string }{
	{"goal", "Objetivo e “pronto quando” preenchidos"},
	{"where_stopped", "“Onde parei” atualizado"},
	{"essential_file", "Pelo menos 1 arquivo essencial com motivo"},
	{"todo_open", "Lista “Falta” não está vazia"},
	{"how_to_test", "“Como testar” preenchido"},
	{"no_open_questions", "Nenhuma pergunta em aberto sem resposta"},
}

// Readiness avalia se o card está pronto para handoff. live indica sessão viva;
// lastEdit é o instante da última edição de arquivo registrada para o card.
func Readiness(c Card, ct Content, live bool, lastEdit *time.Time, s Settings, now time.Time) ReadinessResult {
	var r ReadinessResult
	for _, rule := range readinessRules {
		if !s.Rules[rule.key] {
			continue
		}
		var ok bool
		switch rule.key {
		case "goal":
			ok = c.Goal != "" && c.DoneWhen != ""
		case "where_stopped":
			ok = whereStoppedOK(c, live, lastEdit, s, now)
		case "essential_file":
			for _, f := range ct.Files {
				if f.Why != "" {
					ok = true
					break
				}
			}
		case "todo_open":
			for _, t := range ct.Todos {
				if !t.Done {
					ok = true
					break
				}
			}
		case "how_to_test":
			ok = c.HowToTest != ""
		case "no_open_questions":
			ok = true
			for _, q := range ct.Questions {
				if q.Answer == "" {
					ok = false
					break
				}
			}
		}
		r.Items = append(r.Items, ReadinessItem{Rule: rule.key, Label: rule.label, OK: ok})
		r.Total++
		if ok {
			r.Met++
		}
	}
	r.Ready = r.Met == r.Total
	return r
}

func whereStoppedOK(c Card, live bool, lastEdit *time.Time, s Settings, now time.Time) bool {
	if c.WhereStoppedAt == nil || strings.TrimSpace(c.WhereStopped) == "" {
		return false
	}
	if live {
		return now.Sub(*c.WhereStoppedAt) <= time.Duration(s.WhereStoppedTTLMin)*time.Minute
	}
	return lastEdit == nil || !lastEdit.After(*c.WhereStoppedAt)
}
