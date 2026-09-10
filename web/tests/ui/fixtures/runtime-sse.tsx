import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RuntimeSSEHarness } from './runtime-sse-harness'
import { installRuntimeSSEFixture } from './runtime-sse-fixture'

installRuntimeSSEFixture()

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false, staleTime: Infinity } },
})

createRoot(document.getElementById('root')!).render(
  <QueryClientProvider client={queryClient}>
    <RuntimeSSEHarness />
  </QueryClientProvider>,
)
