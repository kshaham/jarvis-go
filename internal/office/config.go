package office

import "time"

// Config holds office manager configurable timeouts and limits.
type Config struct {
	StandupResponseTimeout time.Duration
	StandupNudgeInterval   time.Duration
	StandupMaxNudges       int
	BrainstormTimeout      time.Duration
	BrainstormCooldown     time.Duration
	ReflectionTimeout      time.Duration
	CuriosityTimeout       time.Duration
	CuriosityCooldown      time.Duration
	RecoveryMaxAttempts    int
	RecoveryWindowDuration time.Duration
	StuckThreshold         time.Duration
	// StuckGracePeriod suppresses stuck-agent recovery for the first N
	// seconds after a team spawns. Protects agents that are still
	// processing their initial kickstart from being auto-killed before
	// they get to emit their first event.
	StuckGracePeriod         time.Duration
	TaskAckTimeout           time.Duration
	TaskAckMaxNudges         int
	ReplenishCooldown        time.Duration
	FileLockTTL              time.Duration
	MaxMessages              int
	MaxBugTasks              int
	AuditMaxEntries          int
	AuditRotateSize          int64
	AutoStandupInterval      time.Duration
	IdleCheckInterval        time.Duration
	IdleThreshold            time.Duration
	MeetingInactivityTimeout time.Duration
	WaterCoolerCooldown      time.Duration
	BlockedTriageMaxAttempts int
	ReflectionCooldown       time.Duration
	// AutoKillAfterStuck controls whether agents that exhaust all recovery
	// tiers are automatically stopped rather than left idle burning quota.
	// Default FALSE because "stuck" often means "waiting on slow provider
	// API" (e.g. Z.AI rate-limit) -- killing a healthy agent for that
	// reason is worse than leaving it idle. Operators who want to clean
	// up truly dead agents can flip this on in settings.
	AutoKillAfterStuck bool
	// R19 · WedgedWorkingTimeout is the ceiling for a session that
	// reports itself as "working" (IsSessionWorking == true) but has
	// emitted no tool activity or EventTokens for this long. Catches
	// hung SDK turns where the subprocess never returns — the
	// turn-cap in R18 doesn't help because NumTurns only ticks at
	// turn boundaries. Default 20 minutes. Set to 0 to disable.
	WedgedWorkingTimeout time.Duration

	// --- Loop-engineering controls (perpetual / RALPH mode) -------------
	// These make the perpetual loop adhere to loop-engineering discipline:
	// an objective gate, hard stops, and a task-category blocklist.

	// VerifyCommand is the objective gate command run to decide whether loop
	// work actually passes (e.g. "go test ./... -count=1"). Empty means
	// auto-detect from the working directory (go.mod -> go test, etc.). This
	// is the ground-truth signal that overrides an agent's self-asserted
	// completion -- the Ralph Wiggum defense.
	VerifyCommand string
	// RalphMaxLoopIterations caps how many self-replenishment cycles a
	// perpetual agent runs before the loop pauses for human review. 0 =
	// unlimited (legacy behavior). Default 10. A HARD STOP, per loop
	// engineering: a loop without one runs until someone notices the bill.
	RalphMaxLoopIterations int
	// PerpetualLoopMaxDuration caps the wall-clock lifetime of a perpetual
	// run before it pauses for human review. 0 = unlimited. The second hard
	// stop (R24 removed dollar/token caps for subscription billing, so the
	// binding constraints are iteration + time).
	PerpetualLoopMaxDuration time.Duration
	// BlockedTaskCategories are substrings that, when present in a
	// self-added task title/description, cause the task to be rejected and
	// escalated rather than worked autonomously. Loops belong on small
	// machine-checkable work, not auth/payments/deploy/architecture.
	BlockedTaskCategories []string
	// PermissionReauditInterval is how often the loop re-checks the agent
	// permission scope against a baseline. Default 30 days. 0 = disabled.
	PermissionReauditInterval time.Duration
}

// DefaultConfig returns production defaults matching TypeScript.
func DefaultConfig() *Config {
	return &Config{
		StandupResponseTimeout: 300 * time.Second,
		StandupNudgeInterval:   60 * time.Second,
		StandupMaxNudges:       3,
		BrainstormTimeout:      300 * time.Second,
		BrainstormCooldown:     180 * time.Second,
		ReflectionTimeout:      180 * time.Second,
		CuriosityTimeout:       180 * time.Second,
		CuriosityCooldown:      900 * time.Second,
		RecoveryMaxAttempts:    3,
		RecoveryWindowDuration: 30 * time.Minute,
		StuckThreshold:         300 * time.Second,
		StuckGracePeriod:       3 * time.Minute,
		TaskAckTimeout:         2 * time.Minute,
		TaskAckMaxNudges:       5,
		// The IsSessionWorking guard in CheckForIdleAgents is what prevents
		// piling a second prompt on top of one still being processed -- it
		// skips any agent whose session is mid-turn. So this cooldown only
		// gates re-nudging a TRULY idle agent (turn finished, queue empty).
		// 45s was too aggressive and 3min left idle agents waiting too long
		// between nudges (a real throughput drag on perpetual teams); 90s
		// gets a genuinely-idle agent back to work quickly while the
		// working-session guard still absorbs in-flight turns.
		ReplenishCooldown:        90 * time.Second,
		FileLockTTL:              30 * time.Minute,
		MaxMessages:              50,
		MaxBugTasks:              100,
		AuditMaxEntries:          500,
		AuditRotateSize:          5 * 1024 * 1024,
		AutoStandupInterval:      4 * time.Hour,
		IdleCheckInterval:        15 * time.Second,
		IdleThreshold:            60 * time.Second,
		MeetingInactivityTimeout: 300 * time.Second,
		WaterCoolerCooldown:      5 * time.Minute,
		BlockedTriageMaxAttempts: 3,
		ReflectionCooldown:       10 * time.Minute,
		AutoKillAfterStuck:       false,
		WedgedWorkingTimeout:     20 * time.Minute,
		// Loop-engineering defaults. VerifyCommand empty => auto-detect.
		VerifyCommand:            "",
		RalphMaxLoopIterations:   10,
		PerpetualLoopMaxDuration: 0, // unlimited by default; operators opt in
		BlockedTaskCategories: []string{
			"auth", "authentication", "login", "oauth",
			"payment", "billing", "refund", "invoice",
			"deploy", "production deploy", "release to prod",
			"migration", "schema migration",
			"dependency upgrade", "bump dependency", "upgrade dependencies",
			"architecture rewrite", "rearchitect",
		},
		PermissionReauditInterval: 30 * 24 * time.Hour,
	}
}
