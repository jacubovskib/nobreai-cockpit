-- O lembrete de plano escrito sem registro passa a valer por card: a sessão
-- que troca de card pode ser lembrada de novo pelo plano do card novo.
-- plan_reminded_for guarda o id do card lembrado ("sem-card" para a sessão
-- sem card; "" = nenhum). Quem já foi lembrado fica lembrado no card atual.
ALTER TABLE sessions ADD COLUMN plan_reminded_for TEXT NOT NULL DEFAULT '';
UPDATE sessions SET plan_reminded_for = COALESCE(CAST(card_id AS TEXT), 'sem-card') WHERE plan_reminded = 1;
