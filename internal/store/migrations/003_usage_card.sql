-- O uso fica no card em que a sessão estava quando a mensagem foi ingerida:
-- trocar a sessão de card não leva o custo antigo junto.
ALTER TABLE usage ADD COLUMN card_id INTEGER REFERENCES cards(id);
UPDATE usage SET card_id = (SELECT card_id FROM sessions WHERE sessions.id = usage.session_id);
CREATE INDEX idx_usage_card ON usage(card_id);
