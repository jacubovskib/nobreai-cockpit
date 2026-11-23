-- Janela dona de cada arquivo de subagente (subagents/agent-<id>.jsonl) do
-- transcript lido pela sessão principal: a janela da linha do tool_result do
-- Agent que rodou o subagente ('' = principal). A primeira gravação vale.
CREATE TABLE subagent_windows (
  session_id TEXT,
  path TEXT,
  window_session_id TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (session_id, path)
);
