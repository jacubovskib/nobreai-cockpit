export const AGENTS: Record<string, { name: string; color: string }> = {
  claude: { name: 'Claude Code', color: '#3987E5' },
  codex: { name: 'Codex', color: '#D95926' },
  kiro: { name: 'Kiro', color: '#199E70' },
}

export function agentInfo(id: string) {
  return AGENTS[id] ?? { name: id, color: '#A0A7B4' }
}

export const COLUMN_LABELS: Record<string, string> = {
  backlog: 'Backlog',
  em_andamento: 'Em andamento',
  pausado: 'Pausado / Handoff',
  revisao: 'Revisão',
  concluido: 'Concluído',
}
