-- Memórias do agente: regras curtas com escopo (global, raiz da família ou app).
-- A FK para projects tem ON DELETE CASCADE: excluir o projeto leva as memórias.
-- Uma futura reconstrução de projects (como a 011) precisa preservar esta FK.
CREATE TABLE memories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
  category TEXT NOT NULL CHECK (category IN ('execucao','shell','dominio','diretiva')),
  rule TEXT NOT NULL,
  rule_norm TEXT NOT NULL,
  -- Regra normalizada original: não muda na edição, para a reimportação não recriar o que o usuário editou.
  origin_norm TEXT NOT NULL DEFAULT '',
  do_text TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('ativa','candidata','arquivada','descartada')),
  source TEXT NOT NULL CHECK (source IN ('agente','usuario','napkin','card')),
  source_ref TEXT NOT NULL DEFAULT '',
  position INTEGER NOT NULL DEFAULT 0,
  needs_review INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX idx_memories_scope ON memories(project_id, status);
CREATE INDEX idx_memories_norm ON memories(rule_norm);
CREATE INDEX idx_memories_origin ON memories(origin_norm);
