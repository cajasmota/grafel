import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './styles/globals.css'
import { App } from './App'

// NOTE: Theme class is applied before first paint by the inline script in index.html.
// ThemeProvider (ThemeContext.tsx) takes over from here and keeps class in sync.

const root = document.getElementById('root')
if (!root) throw new Error('Root element not found')

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
