package notify

import (
	"strings"
	"testing"
)

func TestNotificationEscapingHelpers(t *testing.T) {
	if got := htmlText(`<b>@all</b>&`); got != "&lt;b&gt;@all&lt;/b&gt;&amp;" {
		t.Fatalf("htmlText() = %q", got)
	}
	got := discordText(`@everyone *admin* ` + "`code`")
	if strings.Contains(got, "@everyone") || strings.Contains(got, "*admin*") || strings.Contains(got, "`code`") {
		t.Fatalf("discordText() did not neutralize mentions/markdown: %q", got)
	}
}

func TestNilNotifierIsSafe(t *testing.T) {
	var notifier *Notifier
	notifier.NotifyUserRegistered(UserRegisteredEvent{Username: "user"})
	notifier.NotifyTaskExecuted("task", true, nil)
	notifier.NotifyBackupCreated("backup.zip", 1024)
	notifier.NotifyAccessExpiry("user", 3)
}
