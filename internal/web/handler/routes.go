package handler

// NOTE: the RegisterSessionRoutes / RegisterMonitoringRoutes /
// RegisterConfigRoutes / RegisterMiscRoutes helpers that used to live here
// were dead code -- nothing in server.go ever called them. The authoritative
// route registration lives in internal/web/server.go. Routes added here were
// silently ignored. If you want to split route setup out of server.go in
// the future, wire the splitter explicitly in server.go first.
