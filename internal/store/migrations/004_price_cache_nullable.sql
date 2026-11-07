-- Preço de cache passa a aceitar NULL (desconhecido). Antes, campo em branco
-- era gravado como 0; como não dá para distinguir de um 0 explícito, os zeros
-- existentes viram desconhecidos (o uso com cache aparece "sem preço" até o
-- preço ser informado, em vez de custo subestimado em silêncio).
CREATE TABLE prices_new (
  workspace_id TEXT NOT NULL DEFAULT 'local',
  owner_id TEXT NOT NULL DEFAULT 'me',
  model TEXT PRIMARY KEY,
  input_per_m REAL NOT NULL DEFAULT 0,
  output_per_m REAL NOT NULL DEFAULT 0,
  cache_write_per_m REAL,
  cache_read_per_m REAL
);
INSERT INTO prices_new (workspace_id, owner_id, model, input_per_m, output_per_m, cache_write_per_m, cache_read_per_m)
  SELECT workspace_id, owner_id, model, input_per_m, output_per_m,
    NULLIF(cache_write_per_m, 0), NULLIF(cache_read_per_m, 0)
  FROM prices;
DROP TABLE prices;
ALTER TABLE prices_new RENAME TO prices;
