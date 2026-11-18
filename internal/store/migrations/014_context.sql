ALTER TABLE transcript_offsets ADD COLUMN tool_offset INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transcript_offsets ADD COLUMN segment INTEGER NOT NULL DEFAULT 0;
CREATE TABLE tool_outputs (
  session_id TEXT NOT NULL REFERENCES sessions(id),
  path TEXT NOT NULL,
  item_id TEXT NOT NULL,
  tool TEXT NOT NULL,
  key TEXT NOT NULL,
  bytes INTEGER NOT NULL,
  tokens INTEGER NOT NULL,
  in_tokens INTEGER NOT NULL DEFAULT 0,
  turn INTEGER NOT NULL,
  segment INTEGER NOT NULL DEFAULT 0,
  at TEXT NOT NULL,
  is_error INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (session_id, item_id)
);
CREATE INDEX idx_tool_outputs_at ON tool_outputs(at);
CREATE INDEX idx_tool_outputs_path ON tool_outputs(session_id, path, segment);
CREATE TABLE context_turns (
  session_id TEXT NOT NULL REFERENCES sessions(id),
  path TEXT NOT NULL,
  turn INTEGER NOT NULL,
  segment INTEGER NOT NULL DEFAULT 0,
  message_id TEXT NOT NULL,
  at TEXT NOT NULL,
  tokens INTEGER NOT NULL,
  PRIMARY KEY (session_id, path, turn)
);
CREATE INDEX idx_context_turns_at ON context_turns(at);
