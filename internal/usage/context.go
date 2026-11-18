package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ToolOutput é o tamanho de uma saída de ferramenta ou injeção de hook.
// Privacidade: nada do conteúdo sai do leitor, só nome, chave, tamanhos,
// posição (turno, segmento), hora e is_error.
type ToolOutput struct {
	ItemID   string // tool_use_id ou uuid da linha (hook)
	Tool     string // nome da ferramenta, ou "hook"
	Key      string
	Bytes    int64 // bytes de texto da saída
	Tokens   int64 // estimativa da saída
	InTokens int64 // estimativa do input do tool_use (0 para hook)
	Turn     int   // índice do último turno visto antes; -1 se nenhum
	Segment  int   // compactações vistas antes da saída
	At       time.Time
	IsError  bool
}

// ContextTurn é o contexto de um turno (message.id distinto de assistant).
type ContextTurn struct {
	Turn      int
	Segment   int
	MessageID string
	At        time.Time
	Tokens    int64 // input + cache_read + cache_write
}

// ContextState é o estado que atravessa leituras incrementais do mesmo arquivo.
type ContextState struct {
	LastTurn      int // -1 se nenhum
	LastMessageID string
	Segment       int
}

// EstimateTokens estima tokens: ceil(bytes/4) mais 1600 por imagem.
func EstimateTokens(textBytes int64, images int) int64 {
	return (textBytes+3)/4 + int64(images)*1600
}

type ctxBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Text      string          `json:"text"`
}

type ctxLine struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	UUID       string `json:"uuid"`
	Timestamp  string `json:"timestamp"`
	Attachment *struct {
		Type          string          `json:"type"`
		HookEvent     string          `json:"hookEvent"`
		Command       string          `json:"command"`
		Stdout        string          `json:"stdout"`
		BlockingError json.RawMessage `json:"blockingError"`
	} `json:"attachment"`
	Message *struct {
		ID    string `json:"id"`
		Usage *struct {
			Input      int64 `json:"input_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
			Output     int64 `json:"output_tokens"`
		} `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type toolInfo struct {
	name, key string
	inTokens  int64
}

// ParseContext lê de offset até a última linha completa (em fluxo, linhas de
// qualquer tamanho). Offset maior que o arquivo devolve reset=true e lê do
// começo com estado zerado ({LastTurn:-1}). Turnos devolvidos podem repetir
// o último turno do estado (mesmo message.id): o chamador faz upsert.
// O mapa tool_use_id -> (nome, chave) vale só dentro de uma leitura: um
// tool_result cujo tool_use ficou numa leitura anterior sai como "?".
func ParseContext(path string, offset int64, st ContextState) (outs []ToolOutput, turns []ContextTurn, next int64, newState ContextState, reset bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, offset, st, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, offset, st, false, err
	}
	if offset > info.Size() {
		offset, st, reset = 0, ContextState{LastTurn: -1}, true
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, nil, offset, st, reset, err
	}
	next = offset
	tools := map[string]toolInfo{}
	turnIdx := map[int]int{} // índice do turno -> posição em turns
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, rerr := r.ReadBytes('\n')
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break // linha sem \n final não é lida
			}
			return outs, turns, next, st, reset, rerr
		}
		next += int64(len(line))
		var l ctxLine
		if json.Unmarshal(line, &l) != nil {
			continue
		}
		at, perr := time.Parse(time.RFC3339Nano, l.Timestamp)
		if perr != nil {
			at = info.ModTime()
		}
		switch l.Type {
		case "system":
			if l.Subtype == "compact_boundary" {
				st.Segment++
			}
		case "assistant":
			if l.Message == nil {
				continue
			}
			for _, b := range contentBlocks(l.Message.Content) {
				if b.Type == "tool_use" && b.ID != "" {
					name, key := toolKey(b.Name, b.Input)
					tools[b.ID] = toolInfo{name: name, key: key, inTokens: EstimateTokens(int64(len(b.Input)), 0)}
				}
			}
			u := l.Message.Usage
			if u == nil {
				continue
			}
			tokens := u.Input + u.CacheRead + u.CacheWrite
			if tokens+u.Output == 0 {
				continue // mensagem sintética, sem token
			}
			if l.Message.ID != st.LastMessageID || st.LastTurn < 0 {
				st.LastTurn++
				st.LastMessageID = l.Message.ID
			}
			ct := ContextTurn{Turn: st.LastTurn, Segment: st.Segment, MessageID: l.Message.ID, At: at, Tokens: tokens}
			if i, ok := turnIdx[ct.Turn]; ok {
				turns[i] = ct
			} else {
				turnIdx[ct.Turn] = len(turns)
				turns = append(turns, ct)
			}
		case "user":
			if l.Message == nil {
				continue
			}
			for _, b := range contentBlocks(l.Message.Content) {
				if b.Type != "tool_result" {
					continue
				}
				bytes, images := resultSize(b.Content)
				ti, ok := tools[b.ToolUseID]
				if !ok {
					ti = toolInfo{name: "?", key: "?"}
				}
				outs = append(outs, ToolOutput{ItemID: b.ToolUseID, Tool: ti.name, Key: ti.key, Bytes: bytes,
					Tokens: EstimateTokens(bytes, images), InTokens: ti.inTokens, Turn: st.LastTurn, Segment: st.Segment,
					At: at, IsError: b.IsError})
			}
		case "attachment":
			if l.Attachment == nil {
				continue
			}
			if o, ok := hookOutput(l.UUID, l.Attachment.Type, l.Attachment.HookEvent, l.Attachment.Command,
				l.Attachment.Stdout, l.Attachment.BlockingError); ok {
				o.Turn, o.Segment, o.At = st.LastTurn, st.Segment, at
				outs = append(outs, o)
			}
		}
	}
	return outs, turns, next, st, reset, nil
}

// contentBlocks decodifica message.content quando é lista (string vira nada).
func contentBlocks(raw json.RawMessage) []ctxBlock {
	if len(raw) == 0 || raw[0] != '[' {
		return nil
	}
	var bs []ctxBlock
	if json.Unmarshal(raw, &bs) != nil {
		return nil
	}
	return bs
}

// resultSize mede o content de um tool_result: string ou lista de blocos.
func resultSize(raw json.RawMessage) (bytes int64, images int) {
	if len(raw) == 0 {
		return 0, 0
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return int64(len(s)), 0
		}
		return 0, 0
	}
	for _, b := range contentBlocks(raw) {
		switch b.Type {
		case "text":
			bytes += int64(len(b.Text))
		case "image":
			images++
		}
	}
	return bytes, images
}

// hookOutput mede uma injeção de hook; ok=false quando não vira contexto.
func hookOutput(uuid, typ, event, command, stdout string, blocking json.RawMessage) (ToolOutput, bool) {
	o := ToolOutput{ItemID: uuid, Tool: "hook"}
	switch typ {
	case "hook_success":
		if stdout == "" {
			return o, false
		}
		var n int64
		var parsed map[string]json.RawMessage
		if json.Unmarshal([]byte(stdout), &parsed) == nil {
			var hs struct {
				Ctx string `json:"additionalContext"`
			}
			if json.Unmarshal(parsed["hookSpecificOutput"], &hs) != nil {
				return o, false
			}
			n = int64(len(hs.Ctx))
		} else {
			if event != "SessionStart" && event != "UserPromptSubmit" {
				return o, false
			}
			n = int64(len(stdout))
		}
		if n == 0 {
			return o, false
		}
		origin := "outros"
		if strings.Contains(command, "nobre hook") {
			origin = "cockpit"
		}
		o.Key, o.Bytes = event+" · "+origin, n
	case "hook_blocking_error":
		// Aceita texto puro ou {"blockingError":"texto"}.
		var n int64
		var s string
		var obj struct {
			E string `json:"blockingError"`
		}
		if json.Unmarshal(blocking, &s) == nil {
			n = int64(len(s))
		} else if json.Unmarshal(blocking, &obj) == nil {
			n = int64(len(obj.E))
		}
		if n == 0 {
			return o, false
		}
		o.Key, o.Bytes = event+" · bloqueio", n
	default:
		return o, false
	}
	o.Tokens = EstimateTokens(o.Bytes, 0)
	return o, true
}

var (
	reSafeName = regexp.MustCompile(`^[A-Za-z0-9:_.-]{1,64}$`)
	reExt      = regexp.MustCompile(`^\.[A-Za-z0-9]{1,10}$`)
)

// toolKey devolve o nome da ferramenta e a chave sem conteúdo. O input só é
// lido aqui; nada dele sai além da chave.
func toolKey(name string, input json.RawMessage) (string, string) {
	var in struct {
		Command      string `json:"command"`
		FilePath     string `json:"file_path"`
		SubagentType string `json:"subagent_type"`
		Skill        string `json:"skill"`
	}
	_ = json.Unmarshal(input, &in)
	switch name {
	case "Bash":
		return name, BashKey(in.Command)
	case "Read":
		if ext := filepath.Ext(in.FilePath); reExt.MatchString(ext) {
			return name, "Read " + ext
		}
		return name, "Read sem extensão"
	case "Task", "Agent":
		if reSafeName.MatchString(in.SubagentType) {
			return name, "Agent " + in.SubagentType
		}
		return name, "Agent geral"
	case "Skill":
		if reSafeName.MatchString(in.Skill) {
			return name, "Skill " + in.Skill
		}
		return name, "Skill"
	}
	return name, name
}

var (
	reProgram = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,32}$`)
	reSubcmd  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,24}$`)
	reAssign  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	subcmdOf  = map[string]bool{"git": true, "go": true, "npm": true, "pnpm": true, "yarn": true, "npx": true,
		"docker": true, "kubectl": true, "cargo": true, "make": true, "gh": true, "nobre": true,
		"systemctl": true, "pip": true, "uv": true, "bun": true, "deno": true, "terraform": true, "helm": true}
	wrappers = map[string]bool{"sudo": true, "env": true, "time": true, "nice": true, "nohup": true, "rtk": true}
)

// firstSegment corta no primeiro |, ;, && ou || e devolve o trecho e o resto.
func firstSegment(s string) (seg, rest string, cut bool) {
	for i := 0; i < len(s); i++ {
		switch {
		case strings.HasPrefix(s[i:], "&&"), strings.HasPrefix(s[i:], "||"):
			return s[:i], s[i+2:], true
		case s[i] == '|' || s[i] == ';':
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// BashKey reduz um comando Bash a programa (e subcomando conhecido), sem
// argumentos nem conteúdo. Fora do padrão vira "outro".
func BashKey(command string) string {
	s := strings.TrimSpace(command)
	var seg string
	for {
		var rest string
		var cut bool
		seg, rest, cut = firstSegment(s)
		f := strings.Fields(seg)
		if cut && len(f) > 0 && (f[0] == "cd" || f[0] == "export") {
			s = strings.TrimSpace(rest)
			continue
		}
		break
	}
	toks := strings.Fields(seg)
	for len(toks) > 0 {
		t := toks[0]
		switch {
		case reAssign.MatchString(t), wrappers[filepath.Base(t)]:
			toks = toks[1:]
		case filepath.Base(t) == "timeout":
			toks = toks[1:]
			if len(toks) > 0 {
				toks = toks[1:]
			}
		default:
			goto found
		}
	}
found:
	if len(toks) == 0 {
		return "outro"
	}
	prog := filepath.Base(toks[0])
	if !reProgram.MatchString(prog) {
		return "outro"
	}
	if subcmdOf[prog] && len(toks) > 1 && reSubcmd.MatchString(toks[1]) {
		return prog + " " + toks[1]
	}
	return prog
}
