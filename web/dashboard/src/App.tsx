import { Routes, Route, NavLink } from 'react-router-dom'
import AggregateView from './views/AggregateView'
import RunDetailView from './views/RunDetailView'
import IterationDetailView from './views/IterationDetailView'
import RubricView from './views/RubricView'

/**
 * Application shell with top navigation and react-router-dom v6 routes.
 *
 * Routes (API.md §3, design.md Q6):
 *   /                  → AggregateView   (runs grid + score/cache trends)
 *   /runs/:sessionId   → RunDetailView   (per-run drill-down)
 *   /iterations/:n     → IterationDetailView (per-iteration full detail)
 *   /rubric            → RubricView      (active rubric explainer)
 *
 * R5 extension slot: RunDetailView accepts localStorage-based Bearer-token
 * plumbing via the API client (client.ts AUTH_TOKEN_KEY seam).
 */
export default function App() {
  return (
    <div className="min-h-screen bg-gray-50">
      <nav className="bg-white border-b border-gray-200 px-4 py-3 flex gap-6" role="navigation" aria-label="main">
        <NavLink
          to="/"
          end
          className={({ isActive }) =>
            `text-sm font-medium ${isActive ? 'text-blue-600' : 'text-gray-600 hover:text-gray-900'}`
          }
        >
          Runs
        </NavLink>
        <NavLink
          to="/rubric"
          className={({ isActive }) =>
            `text-sm font-medium ${isActive ? 'text-blue-600' : 'text-gray-600 hover:text-gray-900'}`
          }
        >
          Rubric
        </NavLink>
      </nav>
      <main className="p-4">
        <Routes>
          <Route path="/" element={<AggregateView />} />
          <Route path="/runs/:sessionId" element={<RunDetailView />} />
          <Route path="/iterations/:n" element={<IterationDetailView />} />
          <Route path="/rubric" element={<RubricView />} />
        </Routes>
      </main>
    </div>
  )
}
