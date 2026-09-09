import { createRoot } from 'react-dom/client'
import MarkdownRenderer, { type MarkdownRendererProps } from '../../../src/components/markdown/MarkdownRenderer'
import '../../../src/assets/styles/index.css'

const root = createRoot(document.getElementById('root')!)

declare global {
  interface Window {
    renderMarkdownFixture: (props: MarkdownRendererProps) => void
  }
}

// Test-only fixture: no routes, providers, stores, or business services.
window.renderMarkdownFixture = (props) => root.render(
  <main className="mx-auto grid max-w-5xl grid-cols-[minmax(0,1fr)] gap-4 p-8">
    <MarkdownRenderer {...props} />
  </main>,
)
window.renderMarkdownFixture({ content: '# Markdown fixture' })
