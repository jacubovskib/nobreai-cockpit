package store

import (
	"database/sql"
	"errors"
	"time"
)

// Atribuição por janela (NAC-40). Dois processos na mesma conversa escrevem
// no mesmo transcript, que é lido com o id da sessão principal; cada linha
// pertence a uma janela (a principal ou uma filha, claude:<sid>@<pid>). Nas
// colunas window_session_id, vazio = a principal (a sessão da linha); só
// janelas filhas são gravadas com o id.

// windowPromptTTL é quanto um prompt recebido no hook espera a leitura do
// transcript (a leitura vem no fim do turno): os mais velhos são apagados.
const windowPromptTTL = 24 * time.Hour

// WindowPrompt é um prompt recebido no hook UserPromptSubmit de uma janela,
// ainda não casado com uma linha do transcript. TextNorm é o resumo do texto
// normalizado (o texto em si nunca é gravado).
type WindowPrompt struct {
	ID        int64
	SessionID string
	TextNorm  string
	At        time.Time
}

// AddWindowPrompt registra o prompt da sessão sessionID (a janela roteada) e
// apaga, de passagem, os registros com mais de 24 h. textNorm vazio não é
// registrado.
func (s *Store) AddWindowPrompt(sessionID, textNorm string, at time.Time) error {
	if textNorm == "" {
		return nil
	}
	if _, err := s.DB.Exec(`DELETE FROM window_prompts WHERE at < ?`, fmtTime(at.Add(-windowPromptTTL))); err != nil {
		return err
	}
	_, err := s.DB.Exec(`INSERT INTO window_prompts (session_id, text_norm, at) VALUES (?,?,?)`, sessionID, textNorm, fmtTime(at))
	return err
}

// OpenWindowPrompts devolve os prompts ainda não usados da conversa da sessão
// principal mainID (dela e das filhas), em ordem de registro.
func (s *Store) OpenWindowPrompts(mainID string) ([]WindowPrompt, error) {
	rows, err := s.DB.Query(`SELECT id, session_id, text_norm, at FROM window_prompts
		WHERE used=0 AND (session_id=? OR session_id IN (SELECT id FROM sessions WHERE parent_session_id=?))
		ORDER BY id`, mainID, mainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WindowPrompt
	for rows.Next() {
		var p WindowPrompt
		var at string
		if err := rows.Scan(&p.ID, &p.SessionID, &p.TextNorm, &at); err != nil {
			return nil, err
		}
		if p.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// WindowLines devolve as linhas já atribuídas a uma janela filha no
// transcript path lido pela sessão sessionID (uuid → id da janela). Linha
// ausente é da principal.
func (s *Store) WindowLines(sessionID, path string) (map[string]string, error) {
	rows, err := s.DB.Query(`SELECT uuid, window_session_id FROM window_lines WHERE session_id=? AND path=?`, sessionID, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var u, w string
		if err := rows.Scan(&u, &w); err != nil {
			return nil, err
		}
		out[u] = w
	}
	return out, rows.Err()
}

// TurnWindow devolve a janela do turno messageID já gravado no transcript
// path ("" = principal); ok=false se o turno não foi gravado.
func (s *Store) TurnWindow(sessionID, path, messageID string) (window string, ok bool, err error) {
	err = s.DB.QueryRow(`SELECT window_session_id FROM context_turns WHERE session_id=? AND path=? AND message_id=? ORDER BY turn DESC LIMIT 1`,
		sessionID, path, messageID).Scan(&window)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return window, err == nil, err
}

// ContextWindows é a atribuição por janela de uma leitura do transcript.
// ByUUID tem as linhas desta leitura que pertencem a uma janela filha (uuid →
// id da janela); as demais são da principal. UsedPrompts são os prompts de
// window_prompts casados nesta leitura.
type ContextWindows struct {
	ByUUID      map[string]string
	UsedPrompts []int64
}
