-- nobre:foreign_keys=off
-- Sub-projetos: projects ganha parent_id e path passa a aceitar NULL (grupo de
-- repositórios não tem pasta). Reconstrução de tabela (cards, sessions e
-- prompts referenciam projects, por isso roda com FK desligada).
CREATE TABLE projects_new (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  name TEXT NOT NULL,
  path TEXT UNIQUE,
  capture INTEGER NOT NULL DEFAULT 1,
  base_branch TEXT NOT NULL DEFAULT '',
  parent_id INTEGER REFERENCES projects(id)
);
INSERT INTO projects_new (id, workspace_id, owner_id, name, path, capture, base_branch)
  SELECT id, workspace_id, owner_id, name, path, capture, base_branch FROM projects;
DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;
