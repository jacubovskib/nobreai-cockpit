// OtherWindowTag marca a sessão filha: um segundo processo do Claude Code na
// mesma conversa. O uso fica na sessão principal. Sem parent, nada.
export function OtherWindowTag({ parentId }: { parentId?: string }) {
  if (!parentId) return null
  return (
    <span
      title="Segunda janela do Claude na mesma conversa; o uso fica na sessão principal"
      style={{ marginLeft: 8, fontSize: 12, color: 'var(--text-2)', border: '1px solid var(--border)', borderRadius: 999, padding: '1px 8px' }}
    >
      outra janela
    </span>
  )
}
