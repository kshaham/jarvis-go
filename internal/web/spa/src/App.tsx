import { BrowserRouter, Route, Routes } from 'react-router-dom';

import { AgentsScreen } from './screens/AgentsScreen';
import { TasksScreen } from './screens/TasksScreen';
import { StandupScreen } from './screens/StandupScreen';
import { MessagesScreen } from './screens/MessagesScreen';
import { TimelineScreen } from './screens/TimelineScreen';
import { KnowledgeScreen } from './screens/KnowledgeScreen';
import { ReportsScreen } from './screens/ReportsScreen';
import { SettingsScreen } from './screens/SettingsScreen';
import { WhiteboardScreen } from './screens/WhiteboardScreen';
import { MetricsScreen } from './screens/MetricsScreen';
import { InsightsScreen } from './screens/InsightsScreen';
import { MeetingsScreen } from './screens/MeetingsScreen';
import { SpawnScreen } from './screens/SpawnScreen';
import { DevInstancesScreen } from './screens/DevInstancesScreen';
import { FileLocksScreen } from './screens/FileLocksScreen';
import { HandoffsScreen } from './screens/HandoffsScreen';
import { NetworkScreen } from './screens/NetworkScreen';
import { TeamsScreen } from './screens/TeamsScreen';
import { RelayScreen } from './screens/RelayScreen';
import { AddPeerScreen } from './screens/AddPeerScreen';
import { AppShell } from './components/AppShell';

// /office-v2 mounts under that path on the server; basename keeps
// links + the URL bar consistent. Each route swaps the <Outlet> body
// inside <AppShell>; the shell (header, rail, footer, CommandBar)
// persists across navigations so they feel instant.
export function App() {
  return (
    <BrowserRouter basename="/office-v2">
      <Routes>
        <Route element={<AppShell />}>
          <Route index element={<AgentsScreen />} />
          <Route path="tasks" element={<TasksScreen />} />
          <Route path="standup" element={<StandupScreen />} />
          <Route path="chat" element={<MessagesScreen />} />
          <Route path="activity" element={<TimelineScreen />} />
          <Route path="knowledge" element={<KnowledgeScreen />} />
          <Route path="reports" element={<ReportsScreen />} />
          <Route path="settings" element={<SettingsScreen />} />
          <Route path="whiteboard" element={<WhiteboardScreen />} />
          <Route path="metrics" element={<MetricsScreen />} />
          <Route path="insights" element={<InsightsScreen />} />
          <Route path="meetings" element={<MeetingsScreen />} />
          <Route path="spawn" element={<SpawnScreen />} />
          <Route path="dev-instances" element={<DevInstancesScreen />} />
          <Route path="locks" element={<FileLocksScreen />} />
          <Route path="handoffs" element={<HandoffsScreen />} />
          <Route path="network" element={<NetworkScreen />} />
          <Route path="teams" element={<TeamsScreen />} />
          <Route path="relay" element={<RelayScreen />} />
          <Route path="peers/new" element={<AddPeerScreen />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
