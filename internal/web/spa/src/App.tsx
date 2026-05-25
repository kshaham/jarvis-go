import { AgentsScreen } from './screens/AgentsScreen';
import { TeamHeader } from './components/TeamHeader';

export function App() {
  return (
    <div className="min-h-screen flex flex-col">
      <TeamHeader />
      <main className="flex-1 px-6 py-5 max-w-7xl w-full mx-auto">
        <AgentsScreen />
      </main>
      <footer className="px-6 py-3 text-xs text-slate-500 border-t border-white/5">
        jarvis · /office-v2 (React beta) ·{' '}
        <a href="/office" className="text-sky-400 hover:underline">
          back to classic UI
        </a>
      </footer>
    </div>
  );
}
