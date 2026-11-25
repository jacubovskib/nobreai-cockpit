ALTER TABLE cards ADD COLUMN type TEXT NOT NULL DEFAULT 'tarefa';
ALTER TABLE cards ADD COLUMN parent_id INTEGER REFERENCES cards(id);
ALTER TABLE cards ADD COLUMN priority TEXT NOT NULL DEFAULT 'media';
ALTER TABLE cards ADD COLUMN points REAL;
CREATE INDEX idx_cards_parent ON cards(parent_id);
CREATE TABLE card_labels (
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  card_id INTEGER NOT NULL REFERENCES cards(id),
  label TEXT NOT NULL,
  UNIQUE(card_id, label)
);
