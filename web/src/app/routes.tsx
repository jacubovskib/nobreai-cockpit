import { Route, Routes } from 'react-router-dom'
import { Layout } from './Layout'
import { Placeholder } from '../pages/Placeholder'

// As telas reais entram nas Tasks 14 a 16; aqui são marcadores.
export function AppRoutes() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Placeholder title="Sala de controle" />} />
        <Route path="board" element={<Placeholder title="Board" />} />
        <Route path="cards" element={<Placeholder title="Cards" />} />
        <Route path="cards/:key" element={<Placeholder title="Card" />} />
        <Route path="prompts" element={<Placeholder title="Prompts" />} />
        <Route path="analytics" element={<Placeholder title="Analytics" />} />
        <Route path="config" element={<Placeholder title="Configuração" />} />
        <Route path="*" element={<Placeholder title="Página não encontrada" />} />
      </Route>
    </Routes>
  )
}
