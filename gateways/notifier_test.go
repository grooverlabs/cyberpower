package gateways

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsAlertable(t *testing.T) {
	cases := []struct {
		old, new string
		want     bool
	}{
		{"Utility Power", "Battery", true},
		{"Battery", "Utility Power", true},
		{"Utility Power (Charging)", "Battery", true},
		{"Battery", "Utility Power (Charging)", true},
		{"Utility Power", "Utility Power (Charging)", false}, // both utility
		{"", "Battery", false},                                // unknown baseline
		{"Battery", "", false},
		{"Battery", "Battery", false},
	}
	for _, c := range cases {
		if got := IsAlertable(c.old, c.new); got != c.want {
			t.Errorf("IsAlertable(%q, %q) = %v, want %v", c.old, c.new, got, c.want)
		}
	}
}

func TestMessageFor(t *testing.T) {
	on := messageFor(PowerEvent{Serial: "S1", Model: "CP1500AVR", NewState: "Battery", BatteryPct: 87, RuntimeMin: 14})
	if !strings.Contains(on, "on battery") || !strings.Contains(on, "87%") || !strings.Contains(on, "S1") {
		t.Errorf("on-battery msg unexpected: %q", on)
	}
	off := messageFor(PowerEvent{Serial: "S1", Model: "CP1500AVR", NewState: "Utility Power"})
	if !strings.Contains(off, "power restored") {
		t.Errorf("restored msg unexpected: %q", off)
	}
}

// roundTripFunc lets tests stub the HTTP client.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestNotifier(rt roundTripFunc) *Notifier {
	return &Notifier{
		baseURL:    "http://triton.test",
		token:      "tri_test",
		recipients: []string{"+15551234567", "+15555550199"},
		httpClient: &http.Client{Transport: rt, Timeout: 2 * time.Second},
		cooldown:   30 * time.Second,
		lastSent:   make(map[string]time.Time),
	}
}

func TestNotifyWireFormat(t *testing.T) {
	var captured *http.Request
	var body []byte
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		captured = r
		body, _ = io.ReadAll(r.Body)
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	n := newTestNotifier(rt)

	n.Notify(context.Background(), PowerEvent{
		Serial: "CXXJV2019877", Model: "CP1500AVR",
		OldState: "Utility Power", NewState: "Battery",
		BatteryPct: 87, RuntimeMin: 14,
	})

	if captured == nil {
		t.Fatal("no HTTP request captured")
	}
	if captured.URL.String() != "http://triton.test/notifications" {
		t.Errorf("url: %s", captured.URL)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer tri_test" {
		t.Errorf("auth header: %q", got)
	}
	var payload struct {
		Kind           string `json:"kind"`
		IdempotencyKey string `json:"idempotency_key"`
		Recipients     []struct {
			Channel string                 `json:"channel"`
			To      string                 `json:"to"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"recipients"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json: %v; body=%s", err, body)
	}
	if payload.Kind != "sms" {
		t.Errorf("kind: %q", payload.Kind)
	}
	if !strings.HasPrefix(payload.IdempotencyKey, "cyberpower-CXXJV2019877-battery-") {
		t.Errorf("idempotency_key: %q", payload.IdempotencyKey)
	}
	if len(payload.Recipients) != 2 {
		t.Fatalf("want 2 recipients, got %d", len(payload.Recipients))
	}
	r0 := payload.Recipients[0]
	if r0.Channel != "sms" || r0.To != "+15551234567" {
		t.Errorf("recipient[0]: %+v", r0)
	}
	if b, _ := r0.Payload["body"].(string); !strings.Contains(b, "on battery") {
		t.Errorf("payload.body: %v", r0.Payload)
	}
}

func TestNotifyCooldownSuppresses(t *testing.T) {
	var calls int32
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &http.Response{StatusCode: 202, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	n := newTestNotifier(rt)

	ev := PowerEvent{Serial: "S1", Model: "M", OldState: "Utility Power", NewState: "Battery"}
	n.Notify(context.Background(), ev)
	n.Notify(context.Background(), ev) // within cooldown

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected 1 HTTP call (second suppressed), got %d", got)
	}
}

func TestNotifyNilSafe(t *testing.T) {
	var n *Notifier
	n.Notify(context.Background(), PowerEvent{Serial: "x"}) // must not panic
}

func TestNewNotifierDisabledWhenEnvMissing(t *testing.T) {
	t.Setenv("CYBERPOWER_TRITON_URL", "")
	t.Setenv("CYBERPOWER_TRITON_TOKEN", "")
	t.Setenv("CYBERPOWER_SMS_TO", "")
	if NewNotifier() != nil {
		t.Error("expected nil notifier when env unset")
	}
}
