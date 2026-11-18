// Espelha os JSON da API do nobred (snake_case, como o Go emite).
// Datas chegam como string RFC3339.

export type Column = 'backlog' | 'em_andamento' | 'pausado' | 'revisao' | 'concluido'

export type Signal = '' | 'parado' | 'aguardando_revisao' | 'aguardando_voce' | 'bloqueado' | 'pronto_para_retomar'

export interface Health { ok: boolean; version: string }

export interface Project { id: number; name: string; path: string; capture: boolean; base_branch: string; parent_id: number | null }

export interface Card {
  id: number
  key: string
  title: string
  goal: string
  done_when: string
  where_stopped: string
  where_stopped_at: string | null
  how_to_test: string
  branch: string
  branch_head: string
  branch_diverged: boolean
  branch_merged_at: string | null
  // pause_reason: 'limite de custo' quando o limite pausou o card; blocked_by: chave do card bloqueador.
  pause_reason: string
  handoff_incomplete: boolean
  blocked_by: string
  project_id: number
  column: Column
  created_at: string
  updated_at: string
}

export interface Session {
  id: string
  agent: string
  provider_session_id: string
  card_id: number | null
  project_id: number | null
  pid: number
  cwd: string
  branch: string
  git_head: string
  // worktree: topo do worktree git do cwd ('' fora de repositório);
  // worktree_linked: worktree ligado (git worktree add), não o principal.
  worktree: string
  worktree_linked: boolean
  model: string
  transcript_path: string
  full_capture: boolean
  started_at: string
  last_event_at: string
  ended_at: string | null
  end_reason: string
  pid_dead_at: string | null
}

export interface DomainEvent {
  id: number
  session_id: string
  card_id: number | null
  kind: string
  data: unknown
  at: string
}

// GET /api/events?kind=prompt
export interface PromptEvent extends DomainEvent { agent: string; card_key: string }

export interface Decision { id: number; card_id: number; what: string; why: string; session_id: string; at: string }
export interface EssentialFile { id: number; card_id: number; path: string; lines: string; why: string }
export interface Todo { id: number; card_id: number; text: string; done: boolean; session_id: string }
export interface Question { id: number; card_id: number; text: string; answer: string; session_id: string }

export interface Content {
  decisions: Decision[]
  files: EssentialFile[]
  todos: Todo[]
  questions: Question[]
}

export interface Prompt {
  id: number
  name: string
  slug: string
  tag: string
  description: string
  body: string
  agents: string[]
  project_id: number | null
  is_default_handoff: boolean
  uses: number
  last_used_at: string | null
}

// Preço por milhão de tokens (USD). Cache null = desconhecido (uso com cache
// fica "sem preço"); 0 = grátis.
export interface Price {
  model: string
  input_per_m: number
  output_per_m: number
  cache_write_per_m: number | null
  cache_read_per_m: number | null
}

export interface Limit {
  agent: string
  daily_usd: number
  card_usd: number
  warn_pct: number
  action: 'pause' | 'warn'
}

export interface Settings {
  rules: Record<string, boolean>
  where_stopped_ttl_min: number
  grace_min: number
  reminder_edit_threshold: number
  launch_mode: string
  terminal_cmd: string[]
  currency: 'USD' | 'BRL' | string
  brl_rate: number
  incomplete_action: 'ask_agent' | 'mark' | string
  plan_warn_pct: number
  demo: boolean
  agent_creates_cards: boolean
  inject_paused_cards: boolean
}

export interface ReadinessItem { rule: string; label: string; ok: boolean }
export interface ReadinessResult { items: ReadinessItem[]; met: number; total: number; ready: boolean }

export interface Total {
  key: string
  agent: string
  day: string
  sessions: number
  input: number
  output: number
  cache_write: number
  cache_read: number
  cost_usd: number
  unpriced: boolean
}

export interface SessionWithUsage extends Session { usage: Total; unpriced: boolean }

export interface TouchedFile { path: string; kind: 'editado' | 'lido' | string; count: number; last_at: string }

// GET /api/cards (item do board)
export interface CardSummary extends Card {
  agent: string
  session_count: number
  // live: sessão viva (não encerrada e com processo existindo) é dona do card.
  live: boolean
  // pid_dead_at: sem sessão viva, quando o processo da sessão dona sumiu (carência).
  pid_dead_at: string | null
  last_activity_at: string | null
  totals: Total
  readiness: ReadinessResult
  // signal: parado (em andamento/pausado) ou aguardando_revisao, após 24 h sem atividade;
  // aguardando_voce, bloqueado e pronto_para_retomar vêm do card pausado.
  signal: Signal
  signal_since: string | null
}

// GET /api/cards/{key}
export interface CardDetail extends Card {
  content: Content
  // sessions: as ligadas ao card e as que deixaram uso/eventos nele, cada uma
  // com o uso daquele card.
  sessions: SessionWithUsage[]
  readiness: ReadinessResult
  totals: Total
  live: Session | null
  touched_files: TouchedFile[]
}

// Sessão não encerrada; com pid_dead_at o processo já sumiu (não é viva).
export interface ActiveSession extends Session {
  card: Card | null
  last_activity: string
  session_usage: Total
  card_totals: Total
  readiness: ReadinessResult | null
}

export interface AgentSpend {
  agent: string
  spent_usd: number
  daily_usd: number
  pct: number
  warn_pct: number
  action: 'pause' | 'warn' | ''
  unpriced: boolean
}

export interface TodayTotals {
  active_agents: number
  cards_in_progress: number
  cards_without_live_session: number
  cost_usd: number
  cost_unpriced: boolean
  daily_limit_usd: number
  tokens_input: number
  tokens_output: number
  tokens_cache_write: number
  tokens_cache_read: number
  per_agent: AgentSpend[]
}

export interface Alert {
  kind: 'stale' | 'limit_warn' | 'incomplete' | 'unpriced' | 'plan_limit' | string
  card_key: string
  agent: string
  text: string
  // Só plan_limit: quando a janela reinicia (o alerta some depois disso).
  resets_at?: string | null
}

// GET /api/overview
export interface Overview {
  active: ActiveSession[]
  today: TodayTotals
  alerts: Alert[]
  no_card: SessionWithUsage[]
  events: DomainEvent[]
}

// Evento SSE `changed`: data = {"kind": "..."}
export type StreamKind = 'cards' | 'sessions' | 'config' | 'usage' | 'prompts' | string

export interface AgentCheck { key: string; label: string; detail: string; ok: boolean }
export interface AgentDetection { agent: string; detection: { found: boolean; path: string; version: string; checks: AgentCheck[] } }
export interface InstallReport { changed: string[]; backup: string; messages: string[] }

// GET /api/worktrees
export interface CardRef { key: string; title: string; column: Column }

// Sessão no seu worktree: vivas e encerradas nas últimas 24 h. last_activity só nas vivas.
export interface WorktreeSession extends Session { live: boolean; last_activity: string; card: CardRef | null }

export interface Worktree {
  path: string
  head: string
  branch: string
  main: boolean
  bare: boolean
  detached: boolean
  locked: boolean
  prunable: boolean
  lock_reason: string
  prune_reason: string
  sessions: WorktreeSession[]
  // cards: cards não concluídos cujo branch é o do worktree.
  cards: CardRef[]
  // resources: null quando o daemon não mede recursos.
  resources: WorktreeResources | null
}

// Processo que mais consome; cpu_pct em % de um núcleo (200 = dois núcleos).
export interface ProcUsage { pid: number; comm: string; cpu_pct: number; rss_bytes: number }

// measured_at null = nunca medido; pending = medição na fila (bytes é a anterior).
export interface DiskInfo { bytes: number; measured_at: string | null; pending: boolean }

// Consumo dos processos cujo cwd está no worktree (ou descendentes deles).
// ready falso = a CPU ainda não pôde ser medida (uma amostra só).
export interface WorktreeResources {
  ready: boolean
  cpu_pct: number
  rss_bytes: number
  procs: number
  top: ProcUsage[]
  cpu_cores: number
  disk: DiskInfo | null
}

export interface ProjectWorktrees {
  project: Project
  error: string
  worktrees: Worktree[]
  // orphans: sessões não encerradas cujo worktree não existe mais.
  orphans: WorktreeSession[]
}

// GET /api/worktrees/detail?project=&path=
export interface ProcDetail extends ProcUsage { cwd: string }
export interface FileChange { path: string; code: string }
export interface GitCommit { hash: string; subject: string; author: string; at: string }
export interface GitDetail {
  staged: number
  modified: number
  untracked: number
  conflicts: number
  total_files: number
  files: FileChange[]
  commits: GitCommit[]
  // base: branch do worktree principal; compared falso = sem comparação (principal ou HEAD destacado).
  base: string
  compared: boolean
  ahead: number
  behind: number
  error: string
}
export interface WorktreeSessionDetail extends WorktreeSession { usage: Total; unpriced: boolean }
export interface WorktreeEvent { id: number; session_id: string; agent: string; kind: string; text: string; at: string }
export interface WorktreeDetail {
  project: Project
  worktree: Worktree
  processes: ProcDetail[]
  git: GitDetail | null
  sessions: WorktreeSessionDetail[]
  cards: CardRef[]
  events: WorktreeEvent[]
}

// GET /api/plan-limits?hours=24 (janelas do plano do Claude Code, vindas da status line)
export interface PlanWindowState {
  used_pct: number
  resets_at: string | null
  received_at: string
  expired: boolean
}
export interface PlanPoint { at: string; used_pct: number }
export interface PlanLimits {
  five_hour: PlanWindowState | null
  seven_day: PlanWindowState | null
  history: { five_hour: PlanPoint[] | null; seven_day: PlanPoint[] | null }
  warn_pct: number
}

// DELETE /api/worktrees: 200 em sucesso; 409 com needs_confirm pede confirmação.
export interface RemoveWorktreeResult { removed: boolean; branch_deleted: boolean; branch_kept_reason: string }
export interface RemoveNeedsConfirm {
  error: string
  needs_confirm: true
  files: { path: string; code: string }[]
  total_files: number
  processes: { pid: number; name: string }[]
  processes_unknown?: boolean
  git_unknown?: boolean
  // Commits que só o HEAD do worktree alcança (até 20) e o total.
  commits?: { hash: string; subject: string }[]
  total_commits?: number
  rebase_in_progress?: boolean
  // Sessões vivas em outro worktree do mesmo repositório.
  repo_sessions?: number
}
