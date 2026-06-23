package teamtemplates

// BuiltInTemplates returns the pre-built team composition templates.
//
// Each member ships with a distinct Personality, a role-specific
// SystemPrompt that tells the agent which skills to reach for and
// what "done" means, and a curated Skills list that maps to real
// entries in ~/.claude/skills/. The kickstart builder
// (internal/office/kickstart.go) injects these fields into every
// spawned agent's first prompt.
//
// Member-var definitions live in sibling files split by category:
//   - builtin_fullstack.go  → office7Members, fullstackCompactMembers, coreBuilders6Members
//   - builtin_backend.go    → backendAIMembers
//   - builtin_frontend.go   → frontendStudioMembers
//   - builtin_research.go   → researchLabMembers, innovationSprintMembers
//   - builtin_demo.go       → demoJarvisMembers
//   - builtin_looptriad.go  → loopTriadMembers
//
// To add a new builtin: add a new var to the right sibling (or a new
// sibling file for a new category), then add one row to the slice
// below. No other wiring is needed — handlers consume this slice.
func BuiltInTemplates() []TeamTemplate {
	return []TeamTemplate{
		{ID: "demo-jarvis", Name: "Demo Jarvis", Description: "A tiny 3-member demo team (lead + designer + engineer) that runs the full office workflow for real — standups, whiteboard, tasks, chats, meetings, knowledge, deliverables — so every screen fills with genuine data. Powers the one-click \"Demo Jarvis\" button.", Category: "demo", IsBuiltIn: true, Members: demoJarvisMembers},
		{ID: "office-7", Name: "Office (Default)", Description: "The original 7-member dev team: manager, architect, designer, frontend, backend, devops, and QA.", Category: "fullstack", IsBuiltIn: true, Members: office7Members},
		{ID: "backend-ai-6", Name: "Backend / AI", Description: "AI-focused team with ML engineers, prompt engineer, data engineer, API engineer, and AI QA.", Category: "backend", IsBuiltIn: true, Members: backendAIMembers},
		{ID: "frontend-studio-6", Name: "Frontend Studio", Description: "Frontend-focused team with design lead, architect, UI engineer, interaction engineer, accessibility, and performance.", Category: "frontend", IsBuiltIn: true, Members: frontendStudioMembers},
		{ID: "fullstack-compact-5", Name: "Full-stack Compact", Description: "Lean 5-person team: tech lead, frontend dev, backend dev, QA/security, and devops.", Category: "fullstack", IsBuiltIn: true, Members: fullstackCompactMembers},
		{ID: "research-lab-7", Name: "Research Lab", Description: "Deep research team: director, researcher, analyst, POC developer, presentation specialist, brand strategist, and documentation writer. Biweekly sprint reviews.", Category: "research", IsBuiltIn: true, Members: researchLabMembers},
		{ID: "innovation-sprint-6", Name: "Innovation Sprint", Description: "Lean innovation team: market research, rapid prototyping, visual storytelling, documentation, and validation. Built-in sprint review cadence.", Category: "research", IsBuiltIn: true, Members: innovationSprintMembers},
		{ID: "core-builders-6", Name: "Core Builders", Description: "Product-led full-stack team: Vince directs + maintains the codebase wiki. Lisa researches and QAs internal changes. Allen scouts external frameworks and QAs. Kyle designs UI/UX with a liquid-glass aesthetic. Kam writes the backend. Henry writes the frontend.", Category: "fullstack", IsBuiltIn: true, Members: coreBuilders6Members},
		{ID: "loop-triad-3", Name: "Loop Triad (Planner/Maker/Verifier)", Description: "Three standing teammates for the office perpetual-improvement loop: Atlas (Planner) discovers and queues small machine-checkable tasks, Forge (Maker) implements them one at a time, and Sentinel (Verifier) independently re-checks each completion against its acceptance criteria. Designed for loop-roles mode.", Category: "fullstack", IsBuiltIn: true, Members: loopTriadMembers},
	}
}
