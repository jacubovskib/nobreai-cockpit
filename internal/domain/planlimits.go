package domain

import "time"

// Janelas de limite do plano.
const (
	PlanFiveHour = "five_hour"
	PlanSevenDay = "seven_day"
)

// PlanWindow é a leitura de uma janela de limite do plano.
type PlanWindow struct {
	UsedPct  float64
	ResetsAt *time.Time
}

// PlanReading é o que a status line informou numa sessão; janela nil = ausente.
type PlanReading struct {
	SessionID          string
	At                 time.Time
	FiveHour, SevenDay *PlanWindow
}

// PlanWindowState é a última leitura de uma janela. Expired: resets_at já passou.
type PlanWindowState struct {
	UsedPct    float64    `json:"used_pct"`
	ResetsAt   *time.Time `json:"resets_at"`
	ReceivedAt time.Time  `json:"received_at"`
	Expired    bool       `json:"expired"`
}

// PlanPoint é um ponto do histórico de uma janela.
type PlanPoint struct {
	At      time.Time `json:"at"`
	UsedPct float64   `json:"used_pct"`
}
