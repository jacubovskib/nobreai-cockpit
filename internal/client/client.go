// Package client é o cliente HTTP do nobred usado pelo CLI `nobre`.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/proc"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

// Timeout é o prazo de cada chamada.
const Timeout = 2 * time.Second

// AgentPIDHeader identifica o processo do agente nas rotas /api/me.
const AgentPIDHeader = "X-Nobre-Agent-PID"

// AgentSessionHeader leva `<agente>:<id>` da variável de sessão do agente,
// para quando o PID não acha a sessão.
const AgentSessionHeader = "X-Nobre-Agent-Session"

// agentSessionFromEnv devolve `<agente>:<valor>` da primeira variável de
// sessão presente (agentes em ordem alfabética, para ser determinístico).
func agentSessionFromEnv() string {
	envs := agents.SessionEnvs()
	names := make([]string, 0, len(envs))
	for n := range envs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if v := os.Getenv(envs[n]); v != "" {
			return n + ":" + v
		}
	}
	return ""
}

// Client fala com o nobred. PID é o processo do agente (0 = desconhecido).
type Client struct {
	base string
	hc   *http.Client
	PID  int
}

// New cria um cliente para addr (host:porta). O PID do agente vem de
// NOBRE_AGENT_PID ou da árvore de processos (DetectAgentPID).
func New(addr string) *Client {
	return &Client{base: "http://" + addr, hc: &http.Client{Timeout: Timeout}, PID: DetectAgentPID()}
}

// NewBare é New sem a detecção do PID do agente (que percorre /proc): para
// chamadas com prazo curto, como a da status line.
func NewBare(addr string) *Client {
	return &Client{base: "http://" + addr, hc: &http.Client{Timeout: Timeout}}
}

// PlanLimits envia ao daemon o JSON {session_id, rate_limits} da status line.
func (c *Client) PlanLimits(ctx context.Context, body any) error {
	return c.Do(ctx, "POST", "/api/plan-limits", body, nil)
}

// DetectAgentPID: NOBRE_AGENT_PID tem prioridade; senão sobe a árvore de
// processos a partir do pai atual procurando os nomes de todos os adaptadores.
// Não achar devolve 0.
func DetectAgentPID() int {
	if v := os.Getenv("NOBRE_AGENT_PID"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return findAgentPID(proc.Root, os.Getppid(), agents.Static())
}

// findAgentPID sobe a árvore de processos de root a partir de start.
func findAgentPID(root string, start int, names []string) int {
	pid, err := proc.FindAgentAncestor(root, start, names)
	if err != nil {
		return 0
	}
	return pid
}

// APIError é uma resposta de erro do daemon.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }

// Do chama a API: in vira o corpo JSON (se não nil) e a resposta 2xx é
// decodificada em out (se não nil).
func (c *Client) Do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.PID > 0 {
		req.Header.Set(AgentPIDHeader, strconv.Itoa(c.PID))
	}
	if v := agentSessionFromEnv(); v != "" {
		req.Header.Set(AgentSessionHeader, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("não consegui falar com o nobred em %s (ele está rodando?): %w", req.URL.Host, errors.Unwrap(err))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		msg := http.StatusText(resp.StatusCode)
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			msg = e.Error
		}
		return &APIError{Status: resp.StatusCode, Message: msg}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Hook envia um HookCall ao adaptador do agente.
func (c *Client) Hook(ctx context.Context, agent string, call adapter.HookCall) (adapter.HookReply, error) {
	var r adapter.HookReply
	err := c.Do(ctx, "POST", "/api/hooks/"+url.PathEscape(agent), call, &r)
	return r, err
}

// StatusCard é o card do agente em `nobre status`.
type StatusCard struct {
	domain.Card
	ProjectName string                 `json:"project_name"`
	Readiness   domain.ReadinessResult `json:"readiness"`
	Content     domain.Content         `json:"content"`
}

// Status é a resposta de GET /api/me/status.
type Status struct {
	Session domain.Session `json:"session"`
	Card    *StatusCard    `json:"card"`
}

// Status devolve a sessão do agente e o card ligado a ela (nil se não houver).
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	return s, c.Do(ctx, "GET", "/api/me/status", nil, &s)
}

// Start cria um card e liga a sessão a ele.
func (c *Client) Start(ctx context.Context, title, goal, doneWhen string) (domain.Card, error) {
	var card domain.Card
	err := c.Do(ctx, "POST", "/api/me/start", map[string]string{"title": title, "goal": goal, "done_when": doneWhen}, &card)
	return card, err
}

// Resume liga a sessão ao card e devolve o pacote em markdown.
func (c *Client) Resume(ctx context.Context, key string, force bool) (string, error) {
	var out struct {
		Markdown string `json:"markdown"`
	}
	err := c.Do(ctx, "POST", "/api/me/resume", map[string]any{"key": key, "force": force}, &out)
	return out.Markdown, err
}

// Me faz uma escrita simples numa rota /api/me/<action>.
func (c *Client) Me(ctx context.Context, action string, body any) error {
	return c.Do(ctx, "POST", "/api/me/"+action, body, nil)
}

// Ask registra uma pergunta em aberto e devolve a pergunta criada.
func (c *Client) Ask(ctx context.Context, text string) (domain.Question, error) {
	var q domain.Question
	err := c.Do(ctx, "POST", "/api/me/ask", map[string]string{"text": text}, &q)
	return q, err
}

// AnswerQuestion registra a resposta de uma pergunta do card da sessão.
func (c *Client) AnswerQuestion(ctx context.Context, id int64, answer string) error {
	return c.Do(ctx, "POST", "/api/me/answer", map[string]any{"id": id, "answer": answer}, nil)
}

// Block marca o card que bloqueia o card da sessão (key vazia limpa) e devolve o card.
func (c *Client) Block(ctx context.Context, key, why string) (domain.Card, error) {
	body := map[string]string{"key": key}
	if why != "" {
		body["why"] = why
	}
	var card domain.Card
	err := c.Do(ctx, "POST", "/api/me/block", body, &card)
	return card, err
}

// IntegrationRemoved avisa o daemon de que a integração foi removida de
// propósito (o monitor não deve alertar).
func (c *Client) IntegrationRemoved(ctx context.Context, keys []string, origin string) error {
	return c.Do(ctx, "POST", "/api/integration/removed", map[string]any{"keys": keys, "origin": origin}, nil)
}

// SetBranch informa o branch de trabalho do card da sessão.
func (c *Client) SetBranch(ctx context.Context, branch string) error {
	return c.Do(ctx, "POST", "/api/me/branch", map[string]string{"branch": branch}, nil)
}

// SetApp move o card da sessão para um app (sub-projeto) ou, com geral, de volta ao projeto principal.
func (c *Client) SetApp(ctx context.Context, app string, geral bool) (domain.Card, error) {
	var card domain.Card
	body := map[string]any{"app": app, "geral": geral}
	err := c.Do(ctx, "POST", "/api/me/app", body, &card)
	return card, err
}

// SetProjectBase grava o branch base de um projeto.
func (c *Client) SetProjectBase(ctx context.Context, id int64, base string) error {
	return c.Do(ctx, "PATCH", "/api/projects/"+strconv.FormatInt(id, 10), map[string]string{"base_branch": base}, nil)
}

// AddProject cadastra um projeto.
// Reassigned é quantas sessões abertas foram religadas ao projeto.
func (c *Client) AddProject(ctx context.Context, name, path string) (AddedProject, error) {
	var p AddedProject
	err := c.Do(ctx, "POST", "/api/projects", map[string]string{"name": name, "path": path}, &p)
	return p, err
}

// Moved é o histórico que passou do pai para o filho ao ligar os dois.
type Moved struct {
	Sessions int `json:"sessions"`
	Cards    int `json:"cards"`
}

// AddChildProject cadastra um sub-projeto de parentID.
func (c *Client) AddChildProject(ctx context.Context, name, path string, parentID int64) (AddedProject, error) {
	var p AddedProject
	err := c.Do(ctx, "POST", "/api/projects", map[string]any{"name": name, "path": path, "parent_id": parentID}, &p)
	return p, err
}

// AddGroup cadastra um grupo: projeto principal sem pasta.
func (c *Client) AddGroup(ctx context.Context, name string) (AddedProject, error) {
	var p AddedProject
	err := c.Do(ctx, "POST", "/api/projects", map[string]string{"name": name}, &p)
	return p, err
}

// FolderScan espelha GET /api/fs/scan: o que a pasta é e os repositórios dentro.
type FolderScan struct {
	Path        string `json:"path"`
	Git         bool   `json:"git"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Suggestions []struct {
		workspaces.Suggestion
		Registered *domain.Project `json:"registered"`
		Blocked    string          `json:"blocked"`
	} `json:"suggestions"`
}

// ScanFolder pergunta ao daemon o que há na pasta path.
func (c *Client) ScanFolder(ctx context.Context, path string) (FolderScan, error) {
	var s FolderScan
	return s, c.Do(ctx, "GET", "/api/fs/scan?path="+url.QueryEscape(path), nil, &s)
}

// BatchResult é a resposta de POST /api/projects/batch.
type BatchResult struct {
	Project  domain.Project   `json:"project"`
	Children []domain.Project `json:"children"`
	Moved    Moved            `json:"moved"`
}

// ProjectsBatch cadastra o principal de path (grupo, se a pasta não é git)
// sem filhos; é o que o nobre project add faz numa pasta de repositórios.
func (c *Client) ProjectsBatch(ctx context.Context, name, path string) (BatchResult, error) {
	var r BatchResult
	err := c.Do(ctx, "POST", "/api/projects/batch", map[string]any{"name": name, "path": path, "children": []any{}}, &r)
	return r, err
}

// SetProjectParent liga o projeto id ao pai (nil desliga).
func (c *Client) SetProjectParent(ctx context.Context, id int64, parentID *int64) (Moved, error) {
	var out struct {
		Moved Moved `json:"moved"`
	}
	err := c.Do(ctx, "PATCH", "/api/projects/"+strconv.FormatInt(id, 10), map[string]any{"parent_id": parentID}, &out)
	return out.Moved, err
}

// Projects lista os projetos.
func (c *Client) Projects(ctx context.Context) ([]domain.Project, error) {
	var ps []domain.Project
	return ps, c.Do(ctx, "GET", "/api/projects", nil, &ps)
}

// ScanBranch, ScanPlan, ScanSession e ScanCard espelham GET /api/projects/{id}/scan.
type ScanBranch struct {
	Name       string    `json:"name"`
	Subject    string    `json:"subject"`
	Worktree   string    `json:"worktree"`
	LastCommit time.Time `json:"last_commit"`
	Ahead      int       `json:"ahead"`
	Recent     bool      `json:"recent"`
}

type ScanPlan struct {
	Path     string    `json:"path"`
	Open     int       `json:"open"`
	Done     int       `json:"done"`
	Modified time.Time `json:"modified"`
}

type ScanSession struct {
	ID        string     `json:"id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Branch    string     `json:"branch"`
	Cwd       string     `json:"cwd"`
	CostUSD   float64    `json:"cost_usd"`
}

type ScanCard struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Column string `json:"column"`
	Branch string `json:"branch"`
}

// Scan é o levantamento do histórico de um projeto.
type Scan struct {
	Repo     string        `json:"repo"`
	Base     string        `json:"base"`
	Branches []ScanBranch  `json:"branches"`
	Plans    []ScanPlan    `json:"plans"`
	Sessions []ScanSession `json:"sessions"`
	Cards    []ScanCard    `json:"cards"`

	Suggestions []workspaces.Suggestion `json:"suggestions"`
}

// ScanProject pede o levantamento só de leitura do projeto.
func (c *Client) ScanProject(ctx context.Context, id int64) (Scan, error) {
	var s Scan
	return s, c.Do(ctx, "GET", "/api/projects/"+strconv.FormatInt(id, 10)+"/scan", nil, &s)
}

// DismissSuggestions recusa sugestões de sub-projeto (rels relativos à raiz).
func (c *Client) DismissSuggestions(ctx context.Context, id int64, rels []string) error {
	return c.Do(ctx, "POST", "/api/projects/"+strconv.FormatInt(id, 10)+"/suggestions/dismiss", map[string][]string{"rels": rels}, nil)
}

// AddCard cria um card no projeto da sessão sem ligar a sessão a ele.
func (c *Client) AddCard(ctx context.Context, in map[string]string) (domain.Card, error) {
	var card domain.Card
	return card, c.Do(ctx, "POST", "/api/me/cards", in, &card)
}

// Prompts lista os prompts.
func (c *Client) Prompts(ctx context.Context) ([]domain.Prompt, error) {
	var ps []domain.Prompt
	return ps, c.Do(ctx, "GET", "/api/prompts", nil, &ps)
}

// RenderPrompt renderiza o prompt (com as variáveis do card, se informado) e marca o uso.
func (c *Client) RenderPrompt(ctx context.Context, slug, card string) (string, error) {
	var out struct {
		Text string `json:"text"`
	}
	err := c.Do(ctx, "POST", "/api/prompts/"+url.PathEscape(slug)+"/render", map[string]any{"card": card, "use": true}, &out)
	return out.Text, err
}

// SavePrompt cria um prompt.
func (c *Client) SavePrompt(ctx context.Context, p domain.Prompt) (domain.Prompt, error) {
	var out domain.Prompt
	err := c.Do(ctx, "POST", "/api/prompts", p, &out)
	return out, err
}

// AddedProject é a resposta de AddProject: o projeto e as sessões religadas.
type AddedProject struct {
	domain.Project
	Reassigned  int                     `json:"reassigned"`
	Moved       Moved                   `json:"moved"`
	Suggestions []workspaces.Suggestion `json:"suggestions"`
}

// RememberResult é a memória gravada por `nobre lembrar` e as parecidas que já existiam.
type RememberResult struct {
	domain.Memory
	Scope   string `json:"scope"` // app, raiz ou global
	Similar []struct {
		ID   int64  `json:"id"`
		Rule string `json:"rule"`
	} `json:"similar"`
}

// Remember grava uma memória para a sessão: ativa no escopo projeto ou raiz;
// global vira candidata, à espera da aprovação do usuário.
func (c *Client) Remember(ctx context.Context, rule, do, category, scope string) (RememberResult, error) {
	var r RememberResult
	err := c.Do(ctx, "POST", "/api/me/remember", map[string]string{"rule": rule, "do": do, "category": category, "scope": scope}, &r)
	return r, err
}

// SessionMemory é uma memória que vale para a sessão, com o escopo (app, raiz ou global).
type SessionMemory struct {
	domain.Memory
	Scope string `json:"scope"`
}

// Memories lista as memórias ativas da sessão, filtradas por termo (vazio = todas).
func (c *Client) Memories(ctx context.Context, term string) ([]SessionMemory, error) {
	var out []SessionMemory
	err := c.Do(ctx, "GET", "/api/me/memories?q="+url.QueryEscape(term), nil, &out)
	return out, err
}
