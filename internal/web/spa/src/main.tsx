import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Toaster } from 'sonner';
// Self-hosted variable fonts — family names match tokens.css
// (--font-sans / --font-mono). Imported here so they load with the app
// bundle instead of an external CDN.
import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';
import { App } from './App';
import './styles/tokens.css';
import './styles.css';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5_000,
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('Missing #root element');
ReactDOM.createRoot(rootEl).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
      {/* Mounted outside the router so transient toasts (agent stuck,
          relay messages, SSE reconnect) survive route transitions and
          are never torn down by the page-transition AnimatePresence. */}
      <Toaster
        theme="dark"
        position="bottom-right"
        richColors
        closeButton
        toastOptions={{
          style: {
            background: 'var(--lg-panel-bg)',
            border: '1px solid var(--lg-rim-strong)',
            color: 'var(--text-primary)',
            borderRadius: 'var(--radius-md)',
            WebkitBackdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
            backdropFilter: 'blur(var(--lg-blur-chrome)) saturate(var(--lg-saturate))',
            boxShadow: 'var(--elev-popover-shadow), var(--lg-specular-convex)',
          },
        }}
      />
    </QueryClientProvider>
  </React.StrictMode>,
);
