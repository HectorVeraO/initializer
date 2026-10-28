package apikeys

import (
	"testing"
)

func TestNewAPIKeyAlwaysMatchesTheFormatAndSplits(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 300; i++ {
		plain, err := newAPIKey()
		if err != nil {
			t.Fatalf("newAPIKey: %v", err)
		}
		if !keyPattern.MatchString(plain) || len(plain) != 77 {
			t.Fatalf("key %q does not match the format", plain)
		}
		prefix, secret, ok := splitAPIKey(plain)
		if !ok || len(prefix) != 24 || len(secret) != 52 || prefix+"_"+secret != plain {
			t.Fatalf("split = %q %q %v", prefix, secret, ok)
		}
		if seen[plain] {
			t.Fatal("duplicate key generated")
		}
		seen[plain] = true
	}
}
