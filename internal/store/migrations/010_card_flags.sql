-- Marcas do card: motivo da pausa, handoff incompleto e card bloqueador.
ALTER TABLE cards ADD COLUMN pause_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE cards ADD COLUMN handoff_incomplete INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cards ADD COLUMN blocked_by TEXT NOT NULL DEFAULT '';
