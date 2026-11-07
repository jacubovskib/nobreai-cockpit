// Package domain contém os tipos centrais do NobreAI Cockpit.
package domain

import (
	"encoding/json"
	"time"
)

type Project struct {
	ID      int64
	Name    string
	Path    string
	Capture bool
}

type Card struct {
	ID             int64
	Key            string
	Title          string
	Goal           string
	DoneWhen       string
	WhereStopped   string
	WhereStoppedAt *time.Time
	HowToTest      string
	Branch         string
	ProjectID      int64
	Column         Column
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Session struct {
	ID                string
	Agent             string
	ProviderSessionID string
	CardID            *int64
	ProjectID         *int64
	PID               int
	Cwd               string
	Branch            string
	GitHead           string
	Model             string
	TranscriptPath    string
	FullCapture       bool
	StartedAt         time.Time
	LastEventAt       time.Time
	EndedAt           *time.Time
	EndReason         string
}

type Event struct {
	ID        int64
	SessionID string
	CardID    *int64
	Kind      string
	Data      json.RawMessage
	At        time.Time
}

type Decision struct {
	ID        int64
	CardID    int64
	What      string
	Why       string
	SessionID string
	At        time.Time
}

type EssentialFile struct {
	ID     int64
	CardID int64
	Path   string
	Lines  string
	Why    string
}

type Todo struct {
	ID        int64
	CardID    int64
	Text      string
	Done      bool
	SessionID string
}

type Question struct {
	ID        int64
	CardID    int64
	Text      string
	Answer    string
	SessionID string
}

type UsageRow struct {
	SessionID  string
	MessageID  string
	Model      string
	Input      int64
	Output     int64
	CacheWrite int64
	CacheRead  int64
	At         time.Time
}

type Price struct {
	Model          string
	InputPerM      float64
	OutputPerM     float64
	CacheWritePerM float64
	CacheReadPerM  float64
}

// Limit define limites de gasto por agente. Action é "pause" ou "warn".
type Limit struct {
	Agent    string
	DailyUSD float64
	CardUSD  float64
	WarnPct  int
	Action   string
}

type Prompt struct {
	ID               int64
	Name             string
	Slug             string
	Tag              string
	Description      string
	Body             string
	Agents           []string
	ProjectID        *int64
	IsDefaultHandoff bool
	Uses             int
	LastUsedAt       *time.Time
}
