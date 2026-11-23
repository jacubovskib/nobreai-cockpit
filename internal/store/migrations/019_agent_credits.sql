-- Kiro mede gasto em créditos e contexto só em porcentagem da janela.
ALTER TABLE usage ADD COLUMN credits REAL NOT NULL DEFAULT 0;
ALTER TABLE context_turns ADD COLUMN pct REAL; -- NULL = turno sem porcentagem (Claude)
