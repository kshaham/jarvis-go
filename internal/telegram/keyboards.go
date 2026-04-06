package telegram

import (
	"fmt"
	"strings"
)

// Callback data actions.
const (
	ActionPermissionApprove = "perm_ok"
	ActionPermissionDeny    = "perm_no"
	ActionPermissionTrust   = "perm_trust"
	ActionPlanApprove       = "plan_ok"
	ActionPlanDecline       = "plan_no"
)

// PermissionKeyboard builds an inline keyboard for permission prompts.
// Callback data format: "action:shortSessionID" (fits within 64-byte Telegram limit).
func PermissionKeyboard(sessionID string) *InlineKeyboardMarkup {
	short := ShortID(sessionID)
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Approve", CallbackData: fmt.Sprintf("%s:%s", ActionPermissionApprove, short)},
				{Text: "Deny", CallbackData: fmt.Sprintf("%s:%s", ActionPermissionDeny, short)},
				{Text: "Trust Always", CallbackData: fmt.Sprintf("%s:%s", ActionPermissionTrust, short)},
			},
		},
	}
}

// PlanApprovalKeyboard builds an inline keyboard for plan approval.
func PlanApprovalKeyboard(sessionID string) *InlineKeyboardMarkup {
	short := ShortID(sessionID)
	return &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{Text: "Approve Plan", CallbackData: fmt.Sprintf("%s:%s", ActionPlanApprove, short)},
				{Text: "Decline Plan", CallbackData: fmt.Sprintf("%s:%s", ActionPlanDecline, short)},
			},
		},
	}
}

// ParseCallbackData splits callback data into action and params.
// Format: "action:param1:param2"
func ParseCallbackData(data string) (action string, params []string) {
	parts := strings.SplitN(data, ":", 3)
	if len(parts) == 0 {
		return "", nil
	}
	action = parts[0]
	if len(parts) > 1 {
		params = parts[1:]
	}
	return action, params
}

// CallbackDataSize returns the byte length of callback data for validation.
// Telegram has a 64-byte limit on callback_data.
func CallbackDataSize(data string) int {
	return len(data)
}
