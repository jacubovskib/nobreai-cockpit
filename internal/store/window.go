package store

import (
	"database/sql"
	"errors"
	"time"
)

// Atribuição por janela (NAC-40). Dois processos na mesma conversa escrevem
// no mesmo transcript, que é lido com o id da sessão principal; cada linha
// pertence a uma janela (a principal ou uma filha, claude:<sid>@<pid>). Nas
// colunas window_session_id de context_turns, tool_outputs e usage, vazio = a
// principal (a sessão da linha); só janelas filhas são gravadas com o id.
// window_lines guarda as linhas das filhas e, com o id da principal, as raízes
// da principal que penduram num galho de filha.

// windowPromptTTL é quanto um prompt recebido no hook espera a leitura do
// transcript (a leitura vem no fim do turno): os mais velhos são apagados.
const windowPromptTTL = 24 * time.Hour

// WindowPrompt é um prompt recebido no hook UserPromptSubmit de uma janela,
// ainda não casado com uma linha do transcript. TextHash é o resumo do texto
// normalizado (o texto em si nunca é gravado).
type WindowPrompt struct {
	ID        int64
	SessionID string
	TextHash  string
	At        time.Time
}

// AddWindowPrompt registra o prompt da sessão sessionID (a janela roteada):
// um insert só, no caminho do hook. textHash vazio não é registrado.
func (s *Store) AddWindowPrompt(sessionID, textHash string, at time.Time) error {
	if textHash == "" {
		return nil
	}
	_, err := s.DB.Exec(`INSERT INTO window_prompts (session_id, text_hash, at) VALUES (?,?,?)`, sessionID, textHash, fmtTime(at))
	return err
}

// PruneWindowPrompts apaga os prompts registrados há mais de 24 h antes de
// now (usados ou não): a leitura do transcript vem no fim do turno.
func (s *Store) PruneWindowPrompts(now time.Time) error {
	_, err := s.DB.Exec(`DELETE FROM window_prompts WHERE julianday(at) < julianday(?)`, fmtTime(now.Add(-windowPromptTTL)))
	return err
}

// OpenWindowPrompts devolve os prompts ainda não usados da conversa da sessão
// principal mainID (dela e das filhas), em ordem de registro.
func (s *Store) OpenWindowPrompts(mainID string) ([]WindowPrompt, error) {
	rows, err := s.DB.Query(`SELECT id, session_id, text_hash, at FROM window_prompts
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
		if err := rows.Scan(&p.ID, &p.SessionID, &p.TextHash, &at); err != nil {
			return nil, err
		}
		if p.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// WindowLines devolve as linhas já atribuídas no transcript path lido pela
// sessão sessionID (uuid → id da janela): as das filhas e as raízes da
// principal guardadas com o id dela. Linha ausente herda do pai.
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

// TurnOfMessage devolve o turno já gravado do message.id no transcript path
// (índice idx_context_turns_message); ok=false se não há (ver usage.KnownTurn).
func (s *Store) TurnOfMessage(sessionID, path, messageID string) (turn int, ok bool, err error) {
	err = s.DB.QueryRow(`SELECT turn FROM context_turns WHERE session_id=? AND path=? AND message_id=? ORDER BY turn LIMIT 1`,
		sessionID, path, messageID).Scan(&turn)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return turn, err == nil, err
}

// SubagentWindow devolve a janela dona do arquivo de subagente path do
// transcript lido por sessionID ("" = principal; ver ContextWindows.Subagents);
// ok=false se ainda não se sabe (a linha do resultado do Agent não foi lida).
func (s *Store) SubagentWindow(sessionID, path string) (window string, ok bool, err error) {
	err = s.DB.QueryRow(`SELECT window_session_id FROM subagent_windows WHERE session_id=? AND path=?`,
		sessionID, path).Scan(&window)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return window, err == nil, err
}

// ContextWindows é a atribuição por janela de uma leitura do transcript.
// ByUUID tem as linhas desta leitura que pertencem a uma janela filha (uuid →
// id da janela); as demais são de Default ("" = a principal). Default é a
// janela do arquivo inteiro de um subagente (não vai para window_lines). MainRoots são prompts casados com
// a principal cujo pai é de uma filha (vão para window_lines com o id da
// principal, para a releitura não os passar à filha). UsedPrompts são os
// prompts de window_prompts casados nesta leitura. Compactions são as
// compactações desta leitura (segmento → janela que compactou, "" =
// principal), gravadas em context_compactions.
//
// Subagents são os arquivos de subagente cujo Agent teve o resultado nesta
// leitura do principal (caminho → janela da linha do resultado, "" =
// principal), gravados em subagent_windows. A primeira gravação vale: se a
// janela é filha, turnos, saídas e uso já gravados daquele arquivo (lidos
// antes do resultado, com a principal) passam para ela e para o card dela.
type ContextWindows struct {
	ByUUID      map[string]string
	MainRoots   []string
	UsedPrompts []int64
	Compactions map[int]string
	Default     string
	Subagents   map[string]string
	// Rewrite apaga os turnos e as saídas já gravados do path antes de gravar
	// os desta leitura (releitura do começo, com numeração que pode ter
	// mudado). Diferente de reset, window_lines, compactações e uso ficam.
	Rewrite bool
}
