CREATE TABLE projects (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  capture INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  key TEXT NOT NULL UNIQUE,
  project_id INTEGER NOT NULL REFERENCES projects(id),
  title TEXT NOT NULL,
  goal TEXT NOT NULL DEFAULT '',
  done_when TEXT NOT NULL DEFAULT '',
  where_stopped TEXT NOT NULL DEFAULT '',
  where_stopped_at TEXT,
  how_to_test TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '',
  col TEXT NOT NULL DEFAULT 'backlog',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  agent TEXT NOT NULL,
  provider_session_id TEXT NOT NULL DEFAULT '',
  card_id INTEGER REFERENCES cards(id),
  project_id INTEGER REFERENCES projects(id),
  pid INTEGER NOT NULL DEFAULT 0,
  cwd TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '',
  git_head TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  transcript_path TEXT NOT NULL DEFAULT '',
  transcript_offset INTEGER NOT NULL DEFAULT 0,
  full_capture INTEGER NOT NULL DEFAULT 0,
  started_at TEXT NOT NULL,
  last_event_at TEXT NOT NULL,
  ended_at TEXT,
  end_reason TEXT NOT NULL DEFAULT '',
  reminded INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  session_id TEXT NOT NULL REFERENCES sessions(id),
  card_id INTEGER REFERENCES cards(id),
  kind TEXT NOT NULL,
  data TEXT NOT NULL DEFAULT '{}',
  at TEXT NOT NULL
);
CREATE INDEX idx_events_session ON events(session_id, id);
CREATE INDEX idx_events_card ON events(card_id, id);

CREATE TABLE decisions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  card_id INTEGER NOT NULL REFERENCES cards(id),
  what TEXT NOT NULL,
  why TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT '',
  at TEXT NOT NULL
);

CREATE TABLE essential_files (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  card_id INTEGER NOT NULL REFERENCES cards(id),
  path TEXT NOT NULL,
  lines TEXT NOT NULL DEFAULT '',
  why TEXT NOT NULL DEFAULT '',
  UNIQUE(card_id, path)
);

CREATE TABLE todos (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  card_id INTEGER NOT NULL REFERENCES cards(id),
  text TEXT NOT NULL,
  done INTEGER NOT NULL DEFAULT 0,
  session_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE questions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  card_id INTEGER NOT NULL REFERENCES cards(id),
  text TEXT NOT NULL,
  answer TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE usage (
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  session_id TEXT NOT NULL REFERENCES sessions(id),
  message_id TEXT NOT NULL,
  model TEXT NOT NULL DEFAULT '',
  input INTEGER NOT NULL DEFAULT 0,
  output INTEGER NOT NULL DEFAULT 0,
  cache_write INTEGER NOT NULL DEFAULT 0,
  cache_read INTEGER NOT NULL DEFAULT 0,
  at TEXT NOT NULL,
  PRIMARY KEY (session_id, message_id)
);

CREATE TABLE prices (
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  model TEXT PRIMARY KEY,
  input_per_m REAL NOT NULL DEFAULT 0,
  output_per_m REAL NOT NULL DEFAULT 0,
  cache_write_per_m REAL NOT NULL DEFAULT 0,
  cache_read_per_m REAL NOT NULL DEFAULT 0
);

CREATE TABLE limits (
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  agent TEXT PRIMARY KEY,
  daily_usd REAL NOT NULL DEFAULT 0,
  card_usd REAL NOT NULL DEFAULT 0,
  warn_pct INTEGER NOT NULL DEFAULT 80,
  action TEXT NOT NULL DEFAULT 'warn' CHECK (action IN ('pause','warn'))
);

CREATE TABLE prompts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  name TEXT NOT NULL,
  slug TEXT NOT NULL UNIQUE,
  tag TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  agents TEXT NOT NULL DEFAULT '[]',
  project_id INTEGER REFERENCES projects(id),
  is_default_handoff INTEGER NOT NULL DEFAULT 0,
  uses INTEGER NOT NULL DEFAULT 0,
  last_used_at TEXT
);

CREATE TABLE settings (
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT 'null'
);
