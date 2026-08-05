// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package dataverse

import "testing"

func TestEffectiveMaxFileSize(t *testing.T) {
	testCases := []struct {
		name       string
		configured int64
		server     int64
		expected   int64
	}{
		{"no limits anywhere", 0, 0, 0},
		{"server limit only", 0, 21474836480, 21474836480},
		{"configured limit only", 10737418240, 0, 10737418240},
		{"server stricter than configured", 21474836480, 10737418240, 10737418240},
		{"configured stricter than server", 10737418240, 21474836480, 10737418240},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveMaxFileSize(tc.configured, tc.server); got != tc.expected {
				t.Errorf("effectiveMaxFileSize(%v, %v) = %v, expected %v", tc.configured, tc.server, got, tc.expected)
			}
		})
	}
}

func TestParseSetting(t *testing.T) {
	if size, ok := parseSetting("21474836480"); !ok || size != 21474836480 {
		t.Errorf("expected 21474836480, got %v ok=%v", size, ok)
	}
	if _, ok := parseSetting("not a number"); ok {
		t.Error("expected parse failure for a non-numeric setting")
	}
	if _, ok := parseSetting("-1"); ok {
		t.Error("expected parse failure for a negative setting")
	}
}
