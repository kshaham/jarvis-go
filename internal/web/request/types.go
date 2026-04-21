package request

// CreateSessionRequest is the request body for POST /api/sessions.
type CreateSessionRequest struct {
	WorkingDir string `json:"workingDir" validate:"required"`
	Name       string `json:"name,omitempty"`
	Mode       string `json:"mode,omitempty" validate:"omitempty,oneof=claude shell"`
	// OmitSkipPermissions=true spawns the Claude CLI without the
	// --dangerously-skip-permissions flag. Used by the auth-session
	// path in claude-login.js because Claude CLI refuses that flag
	// as root and exits immediately, breaking /login.
	OmitSkipPermissions bool `json:"omitSkipPermissions,omitempty"`
}

// QuickStartRequest is the request body for POST /api/quick-start.
type QuickStartRequest struct {
	CaseName string `json:"caseName" validate:"required"`
	Mode     string `json:"mode,omitempty" validate:"omitempty,oneof=claude shell"`
}

// SessionInputRequest is the request body for POST /api/sessions/:id/input.
type SessionInputRequest struct {
	Input string `json:"input" validate:"required"`
}

// RenameSessionRequest is the request body for PUT /api/sessions/:id/name.
type RenameSessionRequest struct {
	Name string `json:"name" validate:"required"`
}
