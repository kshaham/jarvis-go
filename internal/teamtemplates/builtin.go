package teamtemplates

// BuiltInTemplates returns the 6 pre-built team composition templates.
func BuiltInTemplates() []TeamTemplate {
	return []TeamTemplate{
		{ID: "office-7", Name: "Office (Default)", Description: "The original 7-member dev team: manager, architect, designer, frontend, backend, devops, and QA.", Category: "fullstack", IsBuiltIn: true, Members: office7Members},
		{ID: "backend-ai-6", Name: "Backend / AI", Description: "AI-focused team with ML engineers, prompt engineer, data engineer, API engineer, and AI QA.", Category: "backend", IsBuiltIn: true, Members: backendAIMembers},
		{ID: "frontend-studio-6", Name: "Frontend Studio", Description: "Frontend-focused team with design lead, architect, UI engineer, interaction engineer, accessibility, and performance.", Category: "frontend", IsBuiltIn: true, Members: frontendStudioMembers},
		{ID: "fullstack-compact-5", Name: "Full-stack Compact", Description: "Lean 5-person team: tech lead, frontend dev, backend dev, QA/security, and devops.", Category: "fullstack", IsBuiltIn: true, Members: fullstackCompactMembers},
		{ID: "research-lab-7", Name: "Research Lab", Description: "Deep research team: director, researcher, analyst, POC developer, presentation specialist, brand strategist, and documentation writer. Biweekly sprint reviews.", Category: "research", IsBuiltIn: true, Members: researchLabMembers},
		{ID: "innovation-sprint-6", Name: "Innovation Sprint", Description: "Lean innovation team: market research, rapid prototyping, visual storytelling, documentation, and validation. Built-in sprint review cadence.", Category: "research", IsBuiltIn: true, Members: innovationSprintMembers},
	}
}

var office7Members = []TeamTemplateMember{
	{ID: "max", Name: "Max", Role: "Engineering Manager", Tier: "strategic", Color: "blue", Emoji: "\U0001F9E0"},
	{ID: "sage", Name: "Sage", Role: "Senior Architect", Tier: "strategic", Color: "purple", Emoji: "\U0001F989"},
	{ID: "nova", Name: "Nova", Role: "UI/UX Designer", Tier: "worker", Color: "pink", Emoji: "\U0001F3A8"},
	{ID: "vex", Name: "Vex", Role: "Frontend Engineer", Tier: "worker", Color: "green", Emoji: "\u26A1"},
	{ID: "bolt", Name: "Bolt", Role: "Backend Engineer", Tier: "worker", Color: "orange", Emoji: "\u2699\uFE0F"},
	{ID: "forge", Name: "Forge", Role: "DevOps Engineer", Tier: "worker", Color: "yellow", Emoji: "\U0001F528"},
	{ID: "lint", Name: "Lint", Role: "QA Engineer", Tier: "worker", Color: "red", Emoji: "\U0001F50D"},
}

var backendAIMembers = []TeamTemplateMember{
	{ID: "aria", Name: "Aria", Role: "AI Engineering Lead", Tier: "strategic", Color: "purple", Emoji: "\U0001F9E0"},
	{ID: "tensor", Name: "Tensor", Role: "ML Engineer", Tier: "worker", Color: "blue", Emoji: "\U0001F916"},
	{ID: "prompt", Name: "Prompt", Role: "Prompt Engineer", Tier: "worker", Color: "green", Emoji: "\u270D\uFE0F"},
	{ID: "dataflow", Name: "Dataflow", Role: "Data Engineer", Tier: "worker", Color: "orange", Emoji: "\U0001F4CA"},
	{ID: "nexus", Name: "Nexus", Role: "API Engineer", Tier: "worker", Color: "yellow", Emoji: "\U0001F310"},
	{ID: "guardian", Name: "Guardian", Role: "AI QA / Evaluator", Tier: "worker", Color: "red", Emoji: "\U0001F6E1\uFE0F"},
}

var frontendStudioMembers = []TeamTemplateMember{
	{ID: "pixel", Name: "Pixel", Role: "Design Lead", Tier: "strategic", Color: "pink", Emoji: "\U0001F3A8"},
	{ID: "react", Name: "React", Role: "Frontend Architect", Tier: "strategic", Color: "blue", Emoji: "\u269B\uFE0F"},
	{ID: "craft", Name: "Craft", Role: "UI Engineer", Tier: "worker", Color: "green", Emoji: "\U0001F528"},
	{ID: "flow", Name: "Flow", Role: "Interaction Engineer", Tier: "worker", Color: "orange", Emoji: "\U0001F3AE"},
	{ID: "access", Name: "Access", Role: "Accessibility & Testing", Tier: "worker", Color: "purple", Emoji: "\u267F"},
	{ID: "perf", Name: "Perf", Role: "Performance Engineer", Tier: "worker", Color: "yellow", Emoji: "\u26A1"},
}

var fullstackCompactMembers = []TeamTemplateMember{
	{ID: "captain", Name: "Captain", Role: "Tech Lead", Tier: "strategic", Color: "blue", Emoji: "\U0001F9ED"},
	{ID: "dash", Name: "Dash", Role: "Frontend Developer", Tier: "worker", Color: "green", Emoji: "\U0001F3A8"},
	{ID: "stack", Name: "Stack", Role: "Backend Developer", Tier: "worker", Color: "orange", Emoji: "\u2699\uFE0F"},
	{ID: "shield", Name: "Shield", Role: "QA & Security", Tier: "worker", Color: "red", Emoji: "\U0001F6E1\uFE0F"},
	{ID: "ops", Name: "Ops", Role: "DevOps", Tier: "worker", Color: "yellow", Emoji: "\U0001F680"},
}

var researchLabMembers = []TeamTemplateMember{
	{ID: "atlas", Name: "Atlas", Role: "Research Director", Tier: "strategic", Color: "blue", Emoji: "\U0001F9ED"},
	{ID: "cipher", Name: "Cipher", Role: "Deep Researcher", Tier: "worker", Color: "purple", Emoji: "\U0001F4DA"},
	{ID: "prism", Name: "Prism", Role: "Technical Analyst", Tier: "worker", Color: "green", Emoji: "\U0001F52C"},
	{ID: "proto", Name: "Proto", Role: "POC Developer", Tier: "worker", Color: "orange", Emoji: "\U0001F6E0\uFE0F"},
	{ID: "slide", Name: "Slide", Role: "Presentation Specialist", Tier: "worker", Color: "pink", Emoji: "\U0001F4CA"},
	{ID: "muse", Name: "Muse", Role: "Brand Strategist", Tier: "worker", Color: "yellow", Emoji: "\u2728"},
	{ID: "quill", Name: "Quill", Role: "Documentation Writer", Tier: "worker", Color: "red", Emoji: "\u270D\uFE0F"},
}

var innovationSprintMembers = []TeamTemplateMember{
	{ID: "spark", Name: "Spark", Role: "Innovation Lead", Tier: "strategic", Color: "blue", Emoji: "\U0001F4A1"},
	{ID: "scout", Name: "Scout", Role: "Market Researcher", Tier: "worker", Color: "purple", Emoji: "\U0001F50E"},
	{ID: "flux", Name: "Flux", Role: "Prototype Engineer", Tier: "worker", Color: "orange", Emoji: "\u26A1"},
	{ID: "pitch", Name: "Pitch", Role: "Visual Storyteller", Tier: "worker", Color: "pink", Emoji: "\U0001F3AC"},
	{ID: "brief", Name: "Brief", Role: "Technical Writer", Tier: "worker", Color: "green", Emoji: "\U0001F4DD"},
	{ID: "verify", Name: "Verify", Role: "Validation Analyst", Tier: "worker", Color: "red", Emoji: "\u2705"},
}
