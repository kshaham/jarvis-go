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
		TaskAckTimeout:         2 * time.Minute,
		TaskAckMaxNudges:       5,
		ReplenishCooldown:      5 * time.Minute,
		FileLockTTL:            30 * time.Minute,
		MaxMessages:            50,
		MaxBugTasks:            100,
		AuditMaxEntries:        500,
		AuditRotateSize:        5 * 1024 * 1024,
		AutoStandupInterval:      4 * time.Hour,
		IdleCheckInterval:        60 * time.Second,
		IdleThreshold:            120 * time.Second,
		MeetingInactivityTimeout:  300 * time.Second,
		WaterCoolerCooldown:      5 * time.Minute,
		BlockedTriageMaxAttempts: 3,
		ReflectionCooldown:       10 * time.Minute,
	}
}
