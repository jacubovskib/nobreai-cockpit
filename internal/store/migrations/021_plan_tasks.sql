-- Plano da entrega registrado no card: o arquivo do plano (não único: o mesmo
-- arquivo pode estar em vários cards) e as tarefas informadas pelo agente.
ALTER TABLE cards ADD COLUMN plan_path TEXT NOT NULL DEFAULT '';
ALTER TABLE cards ADD COLUMN plan_updated_at TEXT;

CREATE TABLE plan_tasks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  card_id INTEGER NOT NULL REFERENCES cards(id),
  n INTEGER NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'a_fazer',
  session_id TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL,
  UNIQUE(card_id, n)
);

-- Lembretes de fim de turno já dados na sessão (uma vez por condição).
ALTER TABLE sessions ADD COLUMN plan_reminded INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN task_reminded_for TEXT NOT NULL DEFAULT '';
