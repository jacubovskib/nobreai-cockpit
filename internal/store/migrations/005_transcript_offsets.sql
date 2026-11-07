-- Até onde cada transcript de subagente já foi lido (o transcript principal
-- continua em sessions.transcript_offset).
CREATE TABLE transcript_offsets (
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  session_id TEXT NOT NULL REFERENCES sessions(id),
  path TEXT NOT NULL,
  offset INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (session_id, path)
);
