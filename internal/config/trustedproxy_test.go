package config

import "testing"

func TestValidateTrustedProxy(t *testing.T) {
	valid := []string{"127.0.0.1", "10.0.0.0/8", "::1", "fd00::/8", " 192.0.2.1 "}
	invalid := []string{"", "example.com", "10.0.0.0/64", "999.1.1.1"}
	for _, entry := range valid {
		if err := validateTrustedProxy(entry); err != nil {
			t.Fatalf("validateTrustedProxy(%q) = %v, want nil", entry, err)
		}
	}
	for _, entry := range invalid {
		if err := validateTrustedProxy(entry); err == nil {
			t.Fatalf("validateTrustedProxy(%q) = nil, want an error", entry)
		}
	}
}
