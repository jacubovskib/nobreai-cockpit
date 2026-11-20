-- Sessão filha: segundo processo do Claude vivo na mesma conversa (mesmo
-- session_id do provedor). Guarda o id da sessão principal; vazio na principal.
ALTER TABLE sessions ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT '';
-- O hook acha a sessão de cada processo por (conversa, PID).
CREATE INDEX idx_sessions_provider_pid ON sessions(provider_session_id, pid);
