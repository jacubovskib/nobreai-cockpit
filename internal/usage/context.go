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
// posição (turno, segmento), hora, is_error e os uuids da linha.
//
// UUID/ParentUUID são os da linha que criou o registro nesta leitura: a
// linha assistant do tool_use (o tool_result que o completa não os troca),
// a linha user do tool_result quando o tool_use ficou numa leitura anterior,
// ou a linha attachment do hook.
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
	// Result é true quando o registro traz Bytes/Tokens/IsError do tool_result
	// (hooks sempre true). Registro de tool_use sem resultado nesta leitura sai
	// com Result false e Bytes/Tokens 0. Tool e Key vazios num registro com
	// Result true significam "desconhecido aqui": o armazenamento mantém o que
	// já tem, ou grava "?" se for novo. Registros do mesmo ItemID se completam
	// por upsert entre leituras.
	Result     bool
	UUID       string
	ParentUUID string
}

// ContextTurn é o contexto de um turno (message.id distinto de assistant).
// Uma mensagem ocupa várias linhas com o mesmo message.id: vale a última
// linha com uso, inclusive para o detalhe de cache, o TTL e UUID/ParentUUID
// (os da última linha da mensagem, que é a ponta do galho naquele turno).
type ContextTurn struct {
	Turn       int
	Segment    int
	MessageID  string
	At         time.Time
	Tokens     int64 // input + cache_read + cache_write
	Input      int64
	CacheRead  int64
	CacheWrite int64
	// TTL do cache gravado no turno: "1h" se ephemeral_1h_input_tokens > 0,
	// senão "5m" se ephemeral_5m_input_tokens > 0, senão "".
	TTL        string
	UUID       string
	ParentUUID string
}

// Prompt é uma linha user com texto digitado pelo usuário (não tool_result).
// Text sai do leitor só para casar a linha com o prompt recebido no hook
// (atribuição por janela); não é gravado nem vai para log.
//
// Regra: content string, ou lista com blocos text (juntados com "\n") e sem
// tool_result. Ficam de fora linhas isMeta, isCompactSummary (resumo de
// compactação), texto vazio e o encanamento de comandos de barra: texto que
// começa com "<command-" ou "<local-command".
type Prompt struct {
	UUID       string
	ParentUUID string
	Text       string
	At         time.Time
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
	Type             string `json:"type"`
	Subtype          string `json:"subtype"`
	UUID             string `json:"uuid"`
	ParentUUID       string `json:"parentUuid"`
	IsMeta           bool   `json:"isMeta"`
	IsCompactSummary bool   `json:"isCompactSummary"`
	Timestamp        string `json:"timestamp"`
	Attachment       *struct {
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
			Creation   *struct {
				H1 int64 `json:"ephemeral_1h_input_tokens"`
				M5 int64 `json:"ephemeral_5m_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// ParseContext lê de offset até a última linha completa (em fluxo, linhas de
// qualquer tamanho). Offset maior que o arquivo devolve reset=true e lê do
// começo com estado zerado ({LastTurn:-1}). Turnos devolvidos podem repetir
// o último turno do estado (mesmo message.id): o chamador faz upsert.
// O tool_use emite um registro (Result false) que o tool_result completa na
// mesma leitura; se o tool_use ficou numa leitura anterior, o registro sai com
// Tool/Key vazios e Result true, e o armazenamento faz o upsert por ItemID.
// prompts são as linhas de prompt do usuário (ver Prompt), na ordem do arquivo.
func ParseContext(path string, offset int64, st ContextState) (outs []ToolOutput, turns []ContextTurn, prompts []Prompt, next int64, newState ContextState, reset bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, offset, st, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, nil, offset, st, false, err
	}
	if offset > info.Size() {
		offset, st, reset = 0, ContextState{LastTurn: -1}, true
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, nil, nil, offset, st, reset, err
	}
	next = offset
	outIdx := map[string]int{} // tool_use_id -> posição em outs
	turnIdx := map[int]int{}   // índice do turno -> posição em turns
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, rerr := r.ReadBytes('\n')
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break // linha sem \n final não é lida
			}
			return outs, turns, prompts, next, st, reset, rerr
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
			if u := l.Message.Usage; u != nil {
				tokens := u.Input + u.CacheRead + u.CacheWrite
				if tokens+u.Output > 0 { // mensagem sintética, sem token, não abre turno
					if l.Message.ID != st.LastMessageID || st.LastTurn < 0 {
						st.LastTurn++
						st.LastMessageID = l.Message.ID
					}
					ct := ContextTurn{Turn: st.LastTurn, Segment: st.Segment, MessageID: l.Message.ID, At: at, Tokens: tokens,
						Input: u.Input, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, UUID: l.UUID, ParentUUID: l.ParentUUID}
					if c := u.Creation; c != nil && c.H1 > 0 {
						ct.TTL = "1h"
					} else if c != nil && c.M5 > 0 {
						ct.TTL = "5m"
					}
					if i, ok := turnIdx[ct.Turn]; ok {
						turns[i] = ct
					} else {
						turnIdx[ct.Turn] = len(turns)
						turns = append(turns, ct)
					}
				}
			}
			// O tool_use pertence ao turno desta mesma linha (já aberto acima).
			for _, b := range contentBlocks(l.Message.Content) {
				if b.Type == "tool_use" && b.ID != "" {
					name, key := toolKey(b.Name, b.Input)
					outIdx[b.ID] = len(outs)
					outs = append(outs, ToolOutput{ItemID: b.ID, Tool: name, Key: key,
						InTokens: EstimateTokens(int64(len(b.Input)), 0), Turn: st.LastTurn, Segment: st.Segment, At: at,
						UUID: l.UUID, ParentUUID: l.ParentUUID})
				}
			}
		case "user":
			if l.Message == nil {
				continue
			}
			if text, ok := promptText(l.Message.Content); ok && !l.IsMeta && !l.IsCompactSummary {
				prompts = append(prompts, Prompt{UUID: l.UUID, ParentUUID: l.ParentUUID, Text: text, At: at})
			}
			for _, b := range contentBlocks(l.Message.Content) {
				if b.Type != "tool_result" || b.ToolUseID == "" {
					continue
				}
				bytes, images := resultSize(b.Content)
				if i, ok := outIdx[b.ToolUseID]; ok {
					outs[i].Bytes, outs[i].Tokens, outs[i].IsError, outs[i].Result = bytes, EstimateTokens(bytes, images), b.IsError, true
					continue
				}
				outs = append(outs, ToolOutput{ItemID: b.ToolUseID, Bytes: bytes, Tokens: EstimateTokens(bytes, images),
					Turn: st.LastTurn, Segment: st.Segment, At: at, IsError: b.IsError, Result: true,
					UUID: l.UUID, ParentUUID: l.ParentUUID})
			}
		case "attachment":
			if l.Attachment == nil || l.UUID == "" {
				continue
			}
			if o, ok := hookOutput(l.UUID, l.Attachment.Type, l.Attachment.HookEvent, l.Attachment.Command,
				l.Attachment.Stdout, l.Attachment.BlockingError); ok {
				o.Turn, o.Segment, o.At = st.LastTurn, st.Segment, at
				o.UUID, o.ParentUUID = l.UUID, l.ParentUUID
				outs = append(outs, o)
			}
		}
	}
	return outs, turns, prompts, next, st, reset, nil
}

// promptText devolve o texto de um prompt do usuário (regra em Prompt):
// content string, ou os blocos text de uma lista sem tool_result.
func promptText(raw json.RawMessage) (string, bool) {
	var text string
	switch {
	case len(raw) == 0:
		return "", false
	case raw[0] == '"':
		if json.Unmarshal(raw, &text) != nil {
			return "", false
		}
	default:
		var parts []string
		for _, b := range contentBlocks(raw) {
			switch b.Type {
			case "tool_result":
				return "", false
			case "text":
				parts = append(parts, b.Text)
			}
		}
		text = strings.Join(parts, "\n")
	}
	lead := strings.TrimLeft(text, " \t\r\n")
	if lead == "" || strings.HasPrefix(lead, "<command-") || strings.HasPrefix(lead, "<local-command") {
		return "", false
	}
	return text, true
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
	o.Tokens, o.Result = EstimateTokens(o.Bytes, 0), true
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

// firstSegment corta no primeiro |, ;, quebra de linha, && ou || fora de
// aspas e devolve o trecho e o resto.
func firstSegment(s string) (seg, rest string, cut bool) {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\\':
			i++
		case c == '\'' || c == '"':
			quote = c
		case strings.HasPrefix(s[i:], "&&"), strings.HasPrefix(s[i:], "||"):
			return s[:i], s[i+2:], true
		case c == '|' || c == ';' || c == '\n':
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// shellWords separa em palavras respeitando aspas simples e duplas (uma
// atribuição A="x y" é uma palavra só).
func shellWords(s string) []string {
	var words []string
	var cur strings.Builder
	var quote byte
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote, in = c, true
			cur.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\r':
			if in {
				words = append(words, cur.String())
				cur.Reset()
				in = false
			}
		default:
			in = true
			cur.WriteByte(c)
		}
	}
	if in {
		words = append(words, cur.String())
	}
	return words
}

func allAssignments(f []string) bool {
	for _, w := range f {
		if !reAssign.MatchString(w) {
			return false
		}
	}
	return len(f) > 0
}

// BashKey reduz um comando Bash a programa (e subcomando conhecido), sem
// argumentos nem conteúdo. Fora do padrão vira "outro".
func BashKey(command string) string {
	s := strings.TrimSpace(command)
	var seg string
	for {
		// Subshell ou grupo na frente: ( cd x && make ) e { ...; }
		s = strings.TrimLeft(s, "({ \t")
		var rest string
		var cut bool
		seg, rest, cut = firstSegment(s)
		f := shellWords(seg)
		if cut && len(f) > 0 && (f[0] == "cd" || f[0] == "export" || allAssignments(f)) {
			s = strings.TrimSpace(rest)
			continue
		}
		break
	}
	toks := shellWords(seg)
	// skipFlags pula flags (-x); os de valueFlags levam o valor junto.
	skipFlags := func(valueFlags ...string) {
		for len(toks) > 0 && strings.HasPrefix(toks[0], "-") {
			takes := false
			for _, v := range valueFlags {
				takes = takes || toks[0] == v
			}
			toks = toks[1:]
			if takes && len(toks) > 0 {
				toks = toks[1:]
			}
		}
	}
	for len(toks) > 0 {
		t := toks[0]
		switch {
		case reAssign.MatchString(t):
			toks = toks[1:]
		case filepath.Base(t) == "sudo":
			toks = toks[1:]
			skipFlags("-u", "-g")
		case filepath.Base(t) == "env":
			toks = toks[1:]
			skipFlags("-u")
		case filepath.Base(t) == "timeout":
			toks = toks[1:]
			skipFlags("-s", "-k")
			if len(toks) > 0 {
				toks = toks[1:] // duração
			}
		case wrappers[filepath.Base(t)]:
			toks = toks[1:]
		default:
			goto found
		}
	}
found:
	if len(toks) == 0 {
		return "outro"
	}
	prog := filepath.Base(toks[0])
	if strings.HasPrefix(prog, "-") || !reProgram.MatchString(prog) {
		return "outro"
	}
	if subcmdOf[prog] && len(toks) > 1 && reSubcmd.MatchString(toks[1]) {
		return prog + " " + toks[1]
	}
	return prog
}
