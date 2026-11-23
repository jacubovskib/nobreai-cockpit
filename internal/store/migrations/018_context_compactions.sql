-- Monitor de contexto (NAC-40). Janela que fez cada compactação do
-- transcript: o segmento (número da compactação no arquivo) e a janela dona
-- ('' = principal). Turnos e saídas guardam o segmento do arquivo; o medidor
-- de cada janela ignora as compactações das outras.
CREATE TABLE context_compactions (
  session_id TEXT,
  path TEXT,
  segment INTEGER,
  window_session_id TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (session_id, path, segment)
);
