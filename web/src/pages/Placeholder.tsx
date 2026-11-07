export function Placeholder({ title }: { title: string }) {
  return (
    <section>
      <h1 style={{ fontSize: 24 }}>{title}</h1>
      <p className="placeholder">Em construção</p>
    </section>
  )
}
