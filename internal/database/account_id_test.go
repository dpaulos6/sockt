package database

import (
	"strings"
	"testing"
)

func TestAccountUUIDParsingAndSQL(t *testing.T) {
	const value = "8f6cc9b8-15e2-4ed8-b3da-f615e419ed9a"
	id, err := ParseAccountUUID(value)
	if err != nil || id.IsZero() || id.String() != value {
		t.Fatalf("UUID roundtrip: %v", err)
	}
	sqlValue, err := id.Value()
	if err != nil || sqlValue != value {
		t.Fatal("SQL value mismatch")
	}
	var scanned AccountUUID
	if err := scanned.Scan(sqlValue); err != nil || scanned != id {
		t.Fatal("SQL scan mismatch")
	}
	for _, invalid := range []string{"", "1", "0", "00000000-0000-0000-0000-000000000000", strings.ToUpper(value), " " + value, value + " ", strings.ReplaceAll(value, "-", ""), value[:8] + "_" + value[9:], "gggggggg-15e2-4ed8-b3da-f615e419ed9a"} {
		if _, err := ParseAccountUUID(invalid); err == nil {
			t.Errorf("accepted %q", invalid)
		}
		scanned = id
		if err := scanned.Scan(invalid); err == nil || !scanned.IsZero() {
			t.Errorf("scan did not fail closed: %q", invalid)
		}
	}
	for _, invalid := range []any{nil, int64(1), []byte(value)} {
		scanned = id
		if err := scanned.Scan(invalid); err == nil || !scanned.IsZero() {
			t.Errorf("scan accepted %T", invalid)
		}
	}
	if _, err := (AccountUUID{}).Value(); err == nil {
		t.Fatal("nil UUID SQL value accepted")
	}
}
