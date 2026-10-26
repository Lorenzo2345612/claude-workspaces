package main

import (
	"testing"
	"time"
)

// A badge run that finds the lock taken must exit at once instead of queueing behind it.
func TestNotificationBadgeSkipsWhenAnotherRunHoldsTheLock(t *testing.T) {
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("WSL_DISTRO_NAME", "test")
	held, err := usageLock(badgeLockName, true)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	done := make(chan error, 1)
	go func() { done <- cmdNotificationBadge(true) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cmdNotificationBadge: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cmdNotificationBadge waited for the lock")
	}
	if err := startNativeBadge(true); err != nil {
		t.Fatalf("startNativeBadge: %v", err)
	}
}

func TestBadgeBackingOff(t *testing.T) {
	now := time.Unix(10_000, 0)
	cases := []struct {
		name string
		st   badgeStamp
		want bool
	}{
		{"never failed", badgeStamp{}, false},
		{"failed just now", badgeStamp{FailedAt: now.Unix() - 5}, true},
		{"failed long ago", badgeStamp{FailedAt: now.Unix() - int64(badgeRetryAfter/time.Second) - 1}, false},
	}
	for _, c := range cases {
		if got := badgeBackingOff(c.st, now); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBadgeStampRoundTrip(t *testing.T) {
	t.Setenv("CW_DATA_DIR", t.TempDir())
	want := badgeStamp{Count: 3, At: 42, FailedAt: 7}
	if err := writeBadgeStamp(want); err != nil {
		t.Fatal(err)
	}
	if got := readBadgeStamp(); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// The autosave runs from the status bar: it must never wait behind another indicators update.
func TestAutosaveIndicatorsSkipWhenLockHeld(t *testing.T) {
	t.Setenv("CW_DATA_DIR", t.TempDir())
	held, err := usageLock(indicatorsLockName, true)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	done := make(chan error, 1)
	go func() { done <- tryUpdateNotificationIndicators() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tryUpdateNotificationIndicators waited for the lock")
	}
}
