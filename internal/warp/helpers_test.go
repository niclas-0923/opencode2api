package warp

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormalizeEndpoint(t *testing.T) {
	cases := map[string]string{
		"1.1.1.1":              "1.1.1.1:2408",
		"2606:4700:4700::1111": "[2606:4700:4700::1111]:2408",
		"example.com:500":      "example.com:500",
		"example.com:":         "example.com:2408",
		"  example.com  ":      "example.com:2408",
	}
	for input, want := range cases {
		got, err := normalizeEndpoint(input)
		if err != nil {
			t.Fatalf("normalizeEndpoint(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeEndpoint(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeEndpoint("host:port:extra"); err == nil {
		t.Fatal("normalizeEndpoint accepted a malformed endpoint")
	}
}

func TestPreferIPv6Endpoints(t *testing.T) {
	got := preferIPv6Endpoints([]string{
		"162.159.192.1:2408",
		"[2606:4700::1]:2408",
		"bad",
		"[2606:4700::2]:2408",
		"[::ffff:162.159.192.1]:2408",
	})
	want := []string{"[2606:4700::1]:2408", "[2606:4700::2]:2408", "162.159.192.1:2408"}
	if len(got) != len(want) {
		t.Fatalf("endpoints = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("endpoints = %v, want %v", got, want)
		}
	}
}

func TestParseTrace(t *testing.T) {
	addr, err := parseTrace(strings.NewReader("warp=on\nip=1.1.1.1\n"))
	if err != nil || addr.String() != "1.1.1.1" {
		t.Fatalf("parseTrace = %v, %v", addr, err)
	}
	if _, err := parseTrace(strings.NewReader("warp=off\nip=1.1.1.1\n")); err == nil {
		t.Fatal("parseTrace accepted warp=off")
	}
	if _, err := parseTrace(strings.NewReader("warp=on\nip=192.168.1.1\n")); err == nil {
		t.Fatal("parseTrace accepted a private address")
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("120"); got != 2*time.Minute {
		t.Fatalf("seconds = %v", got)
	}
	if got := parseRetryAfter("0"); got != 0 {
		t.Fatalf("zero = %v", got)
	}
	future := time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > 2*time.Minute {
		t.Fatalf("future date = %v", got)
	}
	past := time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Fatalf("past date = %v", got)
	}
}
