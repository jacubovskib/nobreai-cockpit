-- Worktree git onde a sessão roda: worktree é o topo do worktree (vazio fora
-- de um repositório) e worktree_linked marca um worktree ligado (git worktree
-- add), diferente do principal.
ALTER TABLE sessions ADD COLUMN worktree TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN worktree_linked INTEGER NOT NULL DEFAULT 0;
