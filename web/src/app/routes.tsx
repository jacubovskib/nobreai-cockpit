import { Route, Routes } from 'react-router-dom'
import { Layout } from './Layout'
import { Placeholder } from '../pages/Placeholder'
import { ControlRoom } from '../pages/ControlRoom'
import { Board } from '../pages/Board'
import { CardsList } from '../pages/CardsList'
import { CardPage } from '../pages/CardPage'
import { Analytics } from '../pages/Analytics'
import { Config } from '../pages/Config'
import { Prompts } from '../pages/Prompts'
import { Worktrees } from '../pages/Worktrees'
import { WorktreeDetailPage } from '../pages/WorktreeDetail'

export function AppRoutes() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<ControlRoom />} />
        <Route path="board" element={<Board />} />
        <Route path="cards" element={<CardsList />} />
        <Route path="cards/:key" element={<CardPage />} />
        <Route path="worktrees" element={<Worktrees />} />
        <Route path="worktrees/detalhe" element={<WorktreeDetailPage />} />
        <Route path="prompts" element={<Prompts />} />
        <Route path="analytics" element={<Analytics />} />
        <Route path="config" element={<Config />} />
        <Route path="*" element={<Placeholder title="Página não encontrada" />} />
      </Route>
    </Routes>
  )
}
