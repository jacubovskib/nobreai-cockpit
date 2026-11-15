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
}

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
	ProjectID      int64      `json:"project_id"`
	Column         Column     `json:"column"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
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
