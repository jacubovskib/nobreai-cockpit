-- Base do projeto e acompanhamento do branch de trabalho do card.
ALTER TABLE projects ADD COLUMN base_branch TEXT NOT NULL DEFAULT '';
ALTER TABLE cards ADD COLUMN branch_head TEXT NOT NULL DEFAULT '';
ALTER TABLE cards ADD COLUMN branch_diverged INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cards ADD COLUMN branch_merged_at TEXT;
