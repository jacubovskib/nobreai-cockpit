// Package domain contém os tipos centrais do NobreAI Cockpit.
package domain

import (
	"encoding/json"
	"time"
)

type Project struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Capture bool   `json:"capture"`
	// BaseBranch é o branch base do projeto ("" = detectar pelo git).
	BaseBranch string `json:"base_branch"`
	// ParentID é o projeto-pai (nil = projeto de topo).
	ParentID *int64 `json:"parent_id"`
	// Kind é "repo" ou "group". Um grupo agrupa repositórios e pode ter ou não
	// pasta (Path "" = sem pasta). Não muda depois do cadastro.
	Kind string `json:"kind"`
}

// IsGroup diz se o projeto é um grupo de repositórios.
func (p Project) IsGroup() bool { return p.Kind == "group" }

type Card struct {
	ID             int64      `json:"id"`
	Key            string     `json:"key"`
	Title          string     `json:"title"`
	Goal           string     `json:"goal"`
	DoneWhen       string     `json:"done_when"`
	WhereStopped   string     `json:"where_stopped"`
	WhereStoppedAt *time.Time `json:"where_stopped_at"`
	HowToTest      string     `json:"how_to_test"`
	Branch         string     `json:"branch"`
	// BranchHead é o último commit visto no branch do card; BranchDiverged
	// marca divergência do que foi visto; BranchMergedAt, quando o branch entrou na base.
	BranchHead     string     `json:"branch_head"`
	BranchDiverged bool       `json:"branch_diverged"`
	BranchMergedAt *time.Time `json:"branch_merged_at"`
	// PauseReason diz por que o card está pausado ("" = pausa comum);
	// HandoffIncomplete marca sessão encerrada sem "Onde parei"; BlockedBy é a
	// chave do card que bloqueia este.
	PauseReason       string    `json:"pause_reason"`
	HandoffIncomplete bool      `json:"handoff_incomplete"`
	BlockedBy         string    `json:"blocked_by"`
	ProjectID         int64     `json:"project_id"`
	Column            Column    `json:"column"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Session struct {
	ID                string `json:"id"`
	Agent             string `json:"agent"`
	ProviderSessionID string `json:"provider_session_id"`
	CardID            *int64 `json:"card_id"`
	ProjectID         *int64 `json:"project_id"`
	PID               int    `json:"pid"`
	Cwd               string `json:"cwd"`
	Branch            string `json:"branch"`
	GitHead           string `json:"git_head"`
	// Worktree é o topo do worktree git do cwd ("" fora de um repositório);
	// WorktreeLinked marca um worktree ligado (git worktree add).
	Worktree       string     `json:"worktree"`
	WorktreeLinked bool       `json:"worktree_linked"`
	Model          string     `json:"model"`
	TranscriptPath string     `json:"transcript_path"`
	FullCapture    bool       `json:"full_capture"`
	StartedAt      time.Time  `json:"started_at"`
	LastEventAt    time.Time  `json:"last_event_at"`
	EndedAt        *time.Time `json:"ended_at"`
	EndReason      string     `json:"end_reason"`
	// PIDDeadAt é quando a reconciliação notou que o PID não existe mais
	// (início da carência antes de encerrar a sessão); nil = nunca notado.
	PIDDeadAt *time.Time `json:"pid_dead_at"`
	// SeenConfig é a configuração que o agente principal da sessão já viu;
	// nil = ainda não gravada.
	SeenConfig *SeenConfig `json:"seen_config"`
	// ParentSessionID é o id da sessão principal quando esta é a sessão filha
	// de outro processo vivo na mesma conversa ("" na principal).
	ParentSessionID string `json:"parent_session_id"`
}

// SeenConfig é o retrato da configuração visto por uma sessão.
type SeenConfig struct {
	AgentCreatesCards bool `json:"agent_creates_cards"`
	Capture           bool `json:"capture"`
}

// Tipos de linha do config_log.
const (
	LogSettingChanged     = "setting_changed"
	LogProjectCapture     = "project_capture_changed"
	LogIntegrationBroken  = "integration_broken"
	LogIntegrationFixed   = "integration_fixed"
	LogIntegrationIgnored = "integration_ignored"
	LogIntegrationRemoved = "integration_removed"
	LogAgentNotified      = "agent_notified"
	LogRTKOn              = "rtk_on"
	LogRTKOff             = "rtk_off"
)

// ConfigLogEntry é uma linha do registro de mudanças de configuração.
type ConfigLogEntry struct {
	ID   int64           `json:"id"`
	At   time.Time       `json:"at"`
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// Live diz se a sessão está viva: não encerrada e com o processo do agente
// existindo. Sessão com PIDDeadAt está na carência e já não é viva.
func (x Session) Live() bool { return x.EndedAt == nil && x.PIDDeadAt == nil }

type Event struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	CardID    *int64          `json:"card_id"`
	Kind      string          `json:"kind"`
	Data      json.RawMessage `json:"data"`
	At        time.Time       `json:"at"`
}

type Decision struct {
	ID        int64     `json:"id"`
	CardID    int64     `json:"card_id"`
	What      string    `json:"what"`
	Why       string    `json:"why"`
	SessionID string    `json:"session_id"`
	At        time.Time `json:"at"`
}

type EssentialFile struct {
	ID     int64  `json:"id"`
	CardID int64  `json:"card_id"`
	Path   string `json:"path"`
	Lines  string `json:"lines"`
	Why    string `json:"why"`
}

type Todo struct {
	ID        int64  `json:"id"`
	CardID    int64  `json:"card_id"`
	Text      string `json:"text"`
	Done      bool   `json:"done"`
	SessionID string `json:"session_id"`
}

type Question struct {
	ID        int64  `json:"id"`
	CardID    int64  `json:"card_id"`
	Text      string `json:"text"`
	Answer    string `json:"answer"`
	SessionID string `json:"session_id"`
}

type UsageRow struct {
	SessionID  string    `json:"session_id"`
	MessageID  string    `json:"message_id"`
	Model      string    `json:"model"`
	Input      int64     `json:"input"`
	Output     int64     `json:"output"`
	CacheWrite int64     `json:"cache_write"`
	CacheRead  int64     `json:"cache_read"`
	At         time.Time `json:"at"`
}

// Price é o preço de um modelo em USD por milhão de tokens. Os preços de cache
// são opcionais: nil = desconhecido (uso com tokens de cache fica "sem preço");
// 0 explícito = grátis.
type Price struct {
	Model          string   `json:"model"`
	InputPerM      float64  `json:"input_per_m"`
	OutputPerM     float64  `json:"output_per_m"`
	CacheWritePerM *float64 `json:"cache_write_per_m"`
	CacheReadPerM  *float64 `json:"cache_read_per_m"`
}

// Limit define limites de gasto por agente. Action é "pause" ou "warn".
type Limit struct {
	Agent    string  `json:"agent"`
	DailyUSD float64 `json:"daily_usd"`
	CardUSD  float64 `json:"card_usd"`
	WarnPct  int     `json:"warn_pct"`
	Action   string  `json:"action"`
}

type Prompt struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	Slug             string     `json:"slug"`
	Tag              string     `json:"tag"`
	Description      string     `json:"description"`
	Body             string     `json:"body"`
	Agents           []string   `json:"agents"`
	ProjectID        *int64     `json:"project_id"`
	IsDefaultHandoff bool       `json:"is_default_handoff"`
	Uses             int        `json:"uses"`
	LastUsedAt       *time.Time `json:"last_used_at"`
}

// Memory é uma regra curta do agente com escopo: ProjectID nil = global.
type Memory struct {
	ID          int64     `json:"id"`
	ProjectID   *int64    `json:"project_id"` // nil = global
	Category    string    `json:"category"`
	Rule        string    `json:"rule"`
	Do          string    `json:"do"`
	Status      string    `json:"status"`
	Source      string    `json:"source"`
	SourceRef   string    `json:"source_ref"` // chave do card ou caminhos do napkin
	Position    int       `json:"position"`
	NeedsReview bool      `json:"needs_review"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
