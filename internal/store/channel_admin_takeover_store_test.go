package store

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestChannelAdminTakeoverNormalizeTruncatesOnRuneBoundary(t *testing.T) {
	now := time.Now()
	item := ChannelAdminTakeover{
		ChannelName: "facebook", ChatID: "customer", AgentKey: "agent",
		LastAdminMessage: strings.Repeat("ảnh", 2500),
		TakenOverAt:      now, ExpiresAt: now.Add(time.Minute),
	}
	if err := item.ValidateActivation(); err != nil {
		t.Fatalf("ValidateActivation: %v", err)
	}
	if !utf8.ValidString(item.LastAdminMessage) || utf8.RuneCountInString(item.LastAdminMessage) != 4000 {
		t.Fatalf("normalized message is invalid or has %d runes", utf8.RuneCountInString(item.LastAdminMessage))
	}
}
