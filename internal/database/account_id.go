package database

import (
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"strings"
)

// AccountUUID is an immutable account identity, distinct from legacy BIGINT keys.
// Its zero value is invalid and never grants admin access. UUIDs are identifiers,
// not credentials. Values are constructed by strict canonical parsing.
type AccountUUID struct{ bytes [16]byte }

func ParseAccountUUID(value string) (AccountUUID, error) {
	var id AccountUUID
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value != strings.ToLower(value) {
		return id, errors.New("account UUID must be a canonical lowercase non-nil UUID")
	}
	raw := value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return id, errors.New("account UUID must be a canonical lowercase non-nil UUID")
	}
	copy(id.bytes[:], decoded)
	if id.IsZero() {
		return AccountUUID{}, errors.New("account UUID must be a canonical lowercase non-nil UUID")
	}
	return id, nil
}

func (id AccountUUID) IsZero() bool { return id == AccountUUID{} }
func (id AccountUUID) String() string {
	raw := hex.EncodeToString(id.bytes[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
}

// Scan validates native PostgreSQL uuid values returned by pgx's SQL driver.
func (id *AccountUUID) Scan(src any) error {
	*id = AccountUUID{}
	value, ok := src.(string)
	if !ok {
		return errors.New("invalid database account UUID")
	}
	parsed, err := ParseAccountUUID(value)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
func (id AccountUUID) Value() (driver.Value, error) {
	if id.IsZero() {
		return nil, errors.New("invalid account UUID")
	}
	return id.String(), nil
}
