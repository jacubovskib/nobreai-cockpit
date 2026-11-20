-- Monitor de contexto (NAC-40). Detalhe de cache por turno: entrada, cache
-- lido, cache gravado e o tempo de vida do cache gravado ('1h', '5m' ou '').
ALTER TABLE context_turns ADD COLUMN input INTEGER NOT NULL DEFAULT 0;
ALTER TABLE context_turns ADD COLUMN cache_read INTEGER NOT NULL DEFAULT 0;
ALTER TABLE context_turns ADD COLUMN cache_write INTEGER NOT NULL DEFAULT 0;
ALTER TABLE context_turns ADD COLUMN ttl TEXT NOT NULL DEFAULT '';
-- Janela (sessão principal ou filha) dona da linha; vazio = a sessão da linha.
ALTER TABLE context_turns ADD COLUMN window_session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE tool_outputs ADD COLUMN window_session_id TEXT NOT NULL DEFAULT '';
ALTER TABLE usage ADD COLUMN window_session_id TEXT NOT NULL DEFAULT '';
-- Uso de uma janela filha (card ligado depois, total por janela) e janela do
-- turno pelo message.id (uso lido depois do contexto).
CREATE INDEX idx_usage_window ON usage(window_session_id);
CREATE INDEX idx_context_turns_message ON context_turns(session_id, path, message_id);
-- Linhas do transcript com janela conhecida entre leituras (o galho de
-- parentUuid herda a janela do pai): as das janelas filhas e, com o id da
-- principal, os prompts da principal que penduram num galho de filha.
CREATE TABLE window_lines (
  session_id TEXT,
  path TEXT,
  uuid TEXT,
  window_session_id TEXT,
  PRIMARY KEY (session_id, path, uuid)
);
-- Prompts recebidos no hook de cada janela, para achar a raiz do galho.
-- text_hash é o sha256 do texto normalizado (o texto nunca é gravado).
CREATE TABLE window_prompts (
  id INTEGER PRIMARY KEY,
  session_id TEXT,
  text_hash TEXT,
  at TEXT,
  used INTEGER NOT NULL DEFAULT 0
);
-- Estado dos avisos ao agente por sessão (sobrevive a reinício).
CREATE TABLE session_alerts (
  session_id TEXT PRIMARY KEY,
  warned_pct INTEGER NOT NULL DEFAULT 0,
  warned_segment INTEGER NOT NULL DEFAULT -1,
  big_outputs INTEGER NOT NULL DEFAULT 0,
  child_notice_sent INTEGER NOT NULL DEFAULT 0
);
