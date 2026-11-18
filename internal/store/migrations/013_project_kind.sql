ALTER TABLE projects ADD COLUMN kind TEXT NOT NULL DEFAULT 'repo';
UPDATE projects SET kind='group' WHERE path IS NULL;
