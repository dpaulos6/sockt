package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"sockt/internal/protocol"
)

const recoveryCount = 8

// Recovery codes are independent random secrets, never derived from a password.
// No plaintext recovery code is ever persisted or logged on the server.
func newRecoveryCode() (string, []byte, error) {
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return "", nil, err
	}
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random)
	code := raw[:8] + "-" + raw[8:16] + "-" + raw[16:24] + "-" + raw[24:]
	hash := sha256.Sum256([]byte(raw))
	return code, hash[:], nil
}

func generateRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64) ([]string, error) {
	codes := make([]string, 0, recoveryCount)
	for i := 0; i < recoveryCount; i++ {
		code, hash, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO recovery_codes(code_hash,user_id) VALUES($1,$2)`, hash, userID); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

func recoveryDigest(code string) ([]byte, error) {
	raw := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if len(raw) != 32 {
		return nil, ErrInvalidRecoveryCode
	}
	if b, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(raw); err != nil || len(b) != 20 {
		return nil, ErrInvalidRecoveryCode
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:], nil
}

func tokenDigest(token string) ([]byte, error) {
	if len(token) != 43 {
		return nil, ErrInvalidSession
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(data) != 32 {
		return nil, ErrInvalidSession
	}
	digest := sha256.Sum256([]byte(token))
	return digest[:], nil
}

func createLabeledSession(ctx context.Context, tx *sql.Tx, id int64, label string) (string, error) {
	if label == "" {
		label = "Unnamed device"
	}
	if !protocol.ValidDeviceLabel(label) {
		return "", errors.New("invalid device label")
	}
	token, hash, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions(token_hash,user_id,expires_at,device_label,last_seen_at)
        VALUES($1,$2,now()+interval '30 days',$3,now())`, hash, id, label)
	return token, err
}

func (s *Store) PasswordLoginDevice(ctx context.Context, username, password, label string) (string, error) {
	if !protocol.ValidUsername(username) {
		return "", ErrInvalidCredentials
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id int64
	var hash string
	// The row lock prevents a concurrent password reset from invalidating
	// the password between verification and session creation.
	err = tx.QueryRowContext(ctx, `SELECT id,password_hash FROM users WHERE username_key=$1 FOR SHARE`, strings.ToLower(username)).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if !VerifyPassword(hash, password) {
		return "", ErrInvalidCredentials
	}
	token, err := createLabeledSession(ctx, tx, id, label)
	if err != nil {
		return "", err
	}
	return token, tx.Commit()
}

func (s *Store) GenerateRecoveryCodes(ctx context.Context, username, token, password string) ([]string, error) {
	if !protocol.ValidUsername(username) {
		return nil, ErrInvalidSession
	}
	digest, err := tokenDigest(token)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	var hash string
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.password_hash FROM users u JOIN sessions s ON s.user_id=u.id
        WHERE u.username_key=$1 AND s.token_hash=$2 AND s.expires_at>now() FOR UPDATE OF u`, strings.ToLower(username), digest).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, err
	}
	if !VerifyPassword(hash, password) {
		return nil, ErrInvalidCredentials
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=$1`, id); err != nil {
		return nil, err
	}
	codes, err := generateRecoveryCodes(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return codes, tx.Commit()
}

func (s *Store) ChangePassword(ctx context.Context, username, token, oldPassword, newPassword, label string) (string, error) {
	if !protocol.ValidUsername(username) {
		return "", ErrInvalidCredentials
	}
	digest, err := tokenDigest(token)
	if err != nil {
		return "", err
	}
	// Hash outside the database transaction to avoid holding locks during Argon2id.
	newHash, err := HashPassword(newPassword)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id int64
	var oldHash string
	err = tx.QueryRowContext(ctx, `SELECT u.id,u.password_hash FROM users u JOIN sessions s ON s.user_id=u.id
        WHERE u.username_key=$1 AND s.token_hash=$2 AND s.expires_at>now() FOR UPDATE OF u`, strings.ToLower(username), digest).Scan(&id, &oldHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidSession
	}
	if err != nil {
		return "", err
	}
	if !VerifyPassword(oldHash, oldPassword) {
		return "", ErrInvalidCredentials
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=$1 WHERE id=$2`, newHash, id); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
		return "", err
	}
	// Force newly authenticated sessions after a password change on every device.
	newToken, err := createLabeledSession(ctx, tx, id, label)
	if err != nil {
		return "", err
	}
	return newToken, tx.Commit()
}

func (s *Store) RecoverPassword(ctx context.Context, username, recoveryCode, newPassword, label string) (string, []string, error) {
	if !protocol.ValidUsername(username) {
		return "", nil, ErrInvalidRecoveryCode
	}
	digest, err := recoveryDigest(recoveryCode)
	if err != nil {
		return "", nil, err
	}
	newHash, err := HashPassword(newPassword)
	if err != nil {
		return "", nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username_key=$1 FOR UPDATE`, strings.ToLower(username)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrInvalidRecoveryCode
	}
	if err != nil {
		return "", nil, err
	}
	var redeemed int64
	err = tx.QueryRowContext(ctx, `UPDATE recovery_codes SET used_at=now()
        WHERE code_hash=$1 AND user_id=$2 AND used_at IS NULL RETURNING user_id`, digest, id).Scan(&redeemed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrInvalidRecoveryCode
	}
	if err != nil {
		return "", nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=$1 WHERE id=$2`, newHash, id); err != nil {
		return "", nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=$1`, id); err != nil {
		return "", nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=$1`, id); err != nil {
		return "", nil, err
	}
	codes, err := generateRecoveryCodes(ctx, tx, id)
	if err != nil {
		return "", nil, err
	}
	newToken, err := createLabeledSession(ctx, tx, id, label)
	if err != nil {
		return "", nil, err
	}
	if err = tx.Commit(); err != nil {
		return "", nil, err
	}
	return newToken, codes, nil
}

func (s *Store) ListSessions(ctx context.Context, username, token string) ([]protocol.SessionInfo, error) {
	if !protocol.ValidUsername(username) {
		return nil, ErrInvalidSession
	}
	digest, err := tokenDigest(token)
	if err != nil {
		return nil, err
	}
	var userID int64
	err = s.db.QueryRowContext(ctx, `SELECT u.id FROM users u JOIN sessions s ON s.user_id=u.id
        WHERE u.username_key=$1 AND s.token_hash=$2 AND s.expires_at>now()`, strings.ToLower(username), digest).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT token_hash,device_label,created_at,last_seen_at,expires_at FROM sessions
        WHERE user_id=$1 AND expires_at>now() ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []protocol.SessionInfo{}
	for rows.Next() {
		var h []byte
		var x protocol.SessionInfo
		if err = rows.Scan(&h, &x.Device, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt); err != nil {
			return nil, err
		}
		x.ID = hex.EncodeToString(h)
		x.Current = subtleCompare(h, digest)
		sessions = append(sessions, x)
	}
	return sessions, rows.Err()
}

func subtleCompare(a, b []byte) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare(a, b) == 1
}

func (s *Store) RevokeSession(ctx context.Context, username, token, sessionID string) (bool, error) {
	if !protocol.ValidUsername(username) {
		return false, ErrInvalidSession
	}
	digest, err := tokenDigest(token)
	if err != nil {
		return false, err
	}
	target, err := hex.DecodeString(sessionID)
	if err != nil || len(target) != 32 {
		return false, errors.New("invalid session identifier")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1 AND user_id=(
        SELECT u.id FROM users u JOIN sessions s ON s.user_id=u.id
        WHERE u.username_key=$2 AND s.token_hash=$3 AND s.expires_at>now())`, target, strings.ToLower(username), digest)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *Store) Logout(ctx context.Context, username, token string) error {
	if !protocol.ValidUsername(username) {
		return ErrInvalidSession
	}
	digest, err := tokenDigest(token)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=$1 AND user_id=(SELECT id FROM users WHERE username_key=$2)`, digest, strings.ToLower(username))
	return err
}

var ErrInvalidRecoveryCode = fmt.Errorf("invalid or already used recovery code")
