import { Suspense, lazy } from 'react';
import { LazyMotion, domMax } from 'motion/react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';

import { AgentsScreen } from './screens/AgentsScreen';
import { TasksScreen } from './screens/TasksScreen';
import { TestsScreen } from './screens/TestsScreen';
import { MockupsScreen } from './screens/MockupsScreen';
import { DeliverablesScreen } from './screens/DeliverablesScreen';
import { StandupScreen } from './screens/StandupScreen';
import { MessagesScreen } from './screens/MessagesScreen';
import { TimelineScreen } from './screens/TimelineScreen';
import { KnowledgeScreen } from './screens/KnowledgeScreen';
import { ReportsScreen } from './screens/ReportsScreen';
import { SettingsScreen } from './screens/SettingsScreen';
import { InsightsScreen } from './screens/InsightsScreen';
import { MeetingsScreen } from './screens/MeetingsScreen';
import { SpawnScreen } from './screens/SpawnScreen';
import { DevInstancesScreen } from './screens/DevInstancesScreen';
import { FileLocksScreen } from './screens/FileLocksScreen';
import { HandoffsScreen } from './screens/HandoffsScreen';
import { TeamsScreen } from './screens/TeamsScreen';
import { RelayScreen } from './screens/RelayScreen';
import { AddPeerScreen } from './screens/AddPeerScreen';
import { AppShell } from './components/AppShell';

// Heavy, dependency-laden screens are code-split so their libraries
// (recharts / @xyflow/react+d3-force / tldraw) stay out of the initial
// bundle and load only when their route is visited. Agents (home) stays
// eager for an instant first paint.
const MetricsScreen = lazy(() =>
  import('./screens/MetricsScreen').then((m) => ({ default: m.MetricsScreen })),
);
const NetworkScreen = lazy(() =>
  import('./screens/NetworkScreen').then((m) => ({ default: m.NetworkScreen })),
);
const WhiteboardScreen = lazy(() =>
  import('./screens/WhiteboardScreen').then((m) => ({ default: m.WhiteboardScreen })),
);

function ScreenFallback() {
  return (
    <div
      style={{
        flex: 1,
        minHeight: 200,
        display: 'grid',
        placeItems: 'center',
        color: 'var(--text-muted)',
        fontSize: 13,
      }}
    >
      Loading…
    </div>
  );
}

// /office-v2 mounts under that path on the server; basename keeps
// links + the URL bar consistent. Each route swaps the animated
// <Outlet> body inside <AppShell>; the shell (header, rail, footer,
// CommandBar, Toaster) persists across navigations.
export function App() {
  return (
    <LazyMotion features={domMax} strict={false}>
    <BrowserRouter basename="/office-v2">
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<AgentsScreen />} />
          <Route path="tasks" element={<TasksScreen />} />
          <Route path="tests" element={<TestsScreen />} />
          <Route path="mockups" element={<MockupsScreen />} />
          <Route path="deliverables" element={<DeliverablesScreen />} />
          <Route path="standup" element={<StandupScreen />} />
          <Route path="chat" element={<MessagesScreen />} />
          <Route path="activity" element={<TimelineScreen />} />
          <Route path="knowledge" element={<KnowledgeScreen />} />
          <Route path="reports" element={<ReportsScreen />} />
          <Route path="settings" element={<SettingsScreen />} />
          <Route
            path="whiteboard"
            element={
              <Suspense fallback={<ScreenFallback />}>
                <WhiteboardScreen />
              </Suspense>
            }
          />
          <Route
            path="metrics"
            element={
              <Suspense fallback={<ScreenFallback />}>
                <MetricsScreen />
              </Suspense>
            }
          />
          <Route path="insights" element={<InsightsScreen />} />
          <Route path="meetings" element={<MeetingsScreen />} />
          <Route path="spawn" element={<SpawnScreen />} />
          <Route path="dev-instances" element={<DevInstancesScreen />} />
          <Route path="locks" element={<FileLocksScreen />} />
          <Route path="handoffs" element={<HandoffsScreen />} />
          <Route
            path="network"
            element={
              <Suspense fallback={<ScreenFallback />}>
                <NetworkScreen />
              </Suspense>
            }
          />
          <Route path="teams" element={<TeamsScreen />} />
          <Route path="relay" element={<RelayScreen />} />
          <Route path="peers/new" element={<AddPeerScreen />} />
        </Route>
      </Routes>
    </BrowserRouter>
    </LazyMotion>
  );
}
