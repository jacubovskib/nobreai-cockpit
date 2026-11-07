import { Route, Routes } from 'react-router-dom'
import { Layout } from './Layout'
import { Placeholder } from '../pages/Placeholder'
import { ControlRoom } from '../pages/ControlRoom'
import { Board } from '../pages/Board'
import { CardsList } from '../pages/CardsList'

// As telas reais entram nas Tasks 15 e 16; aqui são marcadores.
export function AppRoutes() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<ControlRoom />} />
        <Route path="board" element={<Board />} />
        <Route path="cards" element={<CardsList />} />
        <Route path="cards/:key" element={<Placeholder title="Card" />} />
        <Route path="prompts" element={<Placeholder title="Prompts" />} />
        <Route path="analytics" element={<Placeholder title="Analytics" />} />
        <Route path="config" element={<Placeholder title="Configuração" />} />
        <Route path="*" element={<Placeholder title="Página não encontrada" />} />
      </Route>
    </Routes>
  )
}
