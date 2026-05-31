package handler

// Drives the success + send-failure branches of SendTelegramTest by swapping
// the telegramTestSend seam. A configured (connected) bot passes the guard,
// then the seam decides whether the send "succeeds" (200) or "fails" (500).
// The seam is restored after each test so it can't leak across the suite.

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/kshaham/jarvis/internal/telegram"
)

func connectedBotCtx(t *testing.T) *Context {
	t.Helper()
	bot := telegram.New()
	bot.Configure("123456789:secret-bot-token", "chat-1")
	if !bot.IsConnected() {
		t.Fatal("bot should report connected after Configure with token+chatId")
	}
	return &Context{TelegramBot: bot}
}

func TestSendTelegramTest_Success200(t *testing.T) {
	ctx := connectedBotCtx(t)

	orig := telegramTestSend
	t.Cleanup(func() { telegramTestSend = orig })
	var sentText string
	telegramTestSend = func(_ telegramSender, text string) error {
		sentText = text
		return nil
	}

	rec := postJSON(t, ctx.SendTelegramTest, "/api/telegram/test", map[string]interface{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on successful send, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMap(t, rec)
	if resp["success"] != true {
		t.Errorf("success=%v, want true", resp["success"])
	}
	if sentText != "Test from Jarvis" {
		t.Errorf("seam received text=%q, want 'Test from Jarvis'", sentText)
	}
}

func TestSendTelegramTest_SendError500(t *testing.T) {
	ctx := connectedBotCtx(t)

	orig := telegramTestSend
	t.Cleanup(func() { telegramTestSend = orig })
	telegramTestSend = func(_ telegramSender, _ string) error {
		return errors.New("transport boom")
	}

	rec := postJSON(t, ctx.SendTelegramTest, "/api/telegram/test", map[string]interface{}{})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on send failure, got %d: %s", rec.Code, rec.Body.String())
	}
	// The client-facing body must be generic (no underlying error / token leak).
	if got := rec.Body.String(); !strings.Contains(got, "failed to send") {
		t.Errorf("expected generic failure message, got %q", got)
	}
	if strings.Contains(rec.Body.String(), "transport boom") {
		t.Error("client body must not echo the underlying transport error")
	}
}
