-- Até onde cada transcript da sessão (principal e de subagentes) já foi lido,
-- por caminho: um transcript_path novo nunca herda o offset do anterior.
-- (sessions.transcript_offset não é mais usado pelos hooks.)
CREATE TABLE transcript_offsets (
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  session_id TEXT NOT NULL REFERENCES sessions(id),
  path TEXT NOT NULL,
  offset INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (session_id, path)
);
