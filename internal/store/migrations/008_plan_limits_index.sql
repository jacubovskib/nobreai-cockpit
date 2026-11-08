-- Índice para as consultas de "última leitura" e coalescência (por janela e
-- horário de recebimento, como as consultas ordenam).
CREATE INDEX idx_plan_limits_window_received ON plan_limits(window, julianday(received_at));
