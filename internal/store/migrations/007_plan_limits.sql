-- Leituras dos limites do plano (janelas de 5 h e semanal) enviadas pela status
-- line do Claude Code. Uma linha por janela; leituras do mesmo minuto com o
-- mesmo resets_at são coalescidas.
CREATE TABLE plan_limits (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  window TEXT NOT NULL CHECK(window IN ('five_hour','seven_day')),
  used_pct REAL NOT NULL,
  resets_at TEXT,
  received_at TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_plan_limits_window ON plan_limits(window, id);
