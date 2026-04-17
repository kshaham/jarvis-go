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
	StuckGracePeriod       time.Duration
	TaskAckTimeout         time.Duration
	TaskAckMaxNudges       int
	ReplenishCooldown      time.Duration
	FileLockTTL            time.Duration
	MaxMessages            int
	MaxBugTasks            int
	AuditMaxEntries        int
	AuditRotateSize        int64
	AutoStandupInterval      time.Duration
	IdleCheckInterval        time.Duration
	IdleThreshold            time.Duration
	MeetingInactivityTimeout  time.Duration
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
		// 45s was too aggressive -- a 1200-char replenishment prompt
		// typically takes 2-3 minutes for an agent to process and emit
		// `cm task self-add` calls, so the cooldown expired long before
		// the first prompt produced any effect. 3 minutes gives the
		// agent space to finish its turn, and the new IsSessionWorking
		// guard in CheckForIdleAgents prevents piling a second prompt
		// on top of one being processed even within the window.
		ReplenishCooldown:      3 * time.Minute,
		FileLockTTL:            30 * time.Minute,
		MaxMessages:            50,
		MaxBugTasks:            100,
		AuditMaxEntries:        500,
		AuditRotateSize:        5 * 1024 * 1024,
		AutoStandupInterval:      4 * time.Hour,
		IdleCheckInterval:        15 * time.Second,
		IdleThreshold:            60 * time.Second,
		MeetingInactivityTimeout:  300 * time.Second,
		WaterCoolerCooldown:      5 * time.Minute,
		BlockedTriageMaxAttempts: 3,
		ReflectionCooldown:       10 * time.Minute,
		AutoKillAfterStuck:       false,
	}
}
