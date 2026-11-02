import { QueryCache, QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import { App } from './app/App'
import { FeedbackProvider, loadFailed } from './app/feedback'
import { SESSION_KEY } from './app/session'
import { ErrorBoundary } from './app/shared'
import { PreferencesProvider } from './i18n'
import { APIError } from './lib/api'
import './style.css'

const queryClient: QueryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error, query) => {
      if (query.meta?.silent) return
      // The session ended while the page was open: return to sign in.
      if (error instanceof APIError && error.status === 401) {
        queryClient.clear()
        queryClient.setQueryData(SESSION_KEY, null)
        return
      }
      loadFailed(error)
    },
  }),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      // A rejected request will not change; a dropped connection or a waking server may.
      retry: (failures, error) => failures < 2 && !(error instanceof APIError && error.status < 500),
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <PreferencesProvider>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <FeedbackProvider>
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </FeedbackProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  </PreferencesProvider>,
)
