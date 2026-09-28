// Package database is Sockt's PostgreSQL persistence layer. Neither WebSocket transport nor TUI
// code executes SQL. The pgx stdlib driver is registered by cmd/socktd.
package database

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"sockt/internal/protocol"
)

var (
	ErrInvalidInvite       = errors.New("invalid or already used invite")
	ErrInvalidCredentials  = errors.New("invalid username or password")
	ErrInvalidSession      = errors.New("session expired or invalid; run sockt login")
	ErrUsernameUnavailable = errors.New("username already registered")
)

const generalID int64 = 1
const sessionLifetime = 30 * 24 * time.Hour
const dbTimeout = 5 * time.Second

type Identity struct {
	ID       int64
	Username string
}

type Store struct{ db *sql.DB }

func Open(ctx context.Context, dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

// CheckSchema deliberately does NOT run migrations automatically in serve mode.
// Operators apply reviewed schema changes with `socktd migrate` first.
func (s *Store) CheckSchema(ctx context.Context) error {
	var installed bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = '002_accounts.up.sql')`).Scan(&installed)
	if err != nil {
		return fmt.Errorf("schema not initialized; run socktd migrate: %w", err)
	}
	if !installed {
		return errors.New("schema not initialized; run socktd migrate")
	}
	return nil
}

//go:embed migrations/*.up.sql
var migrationFiles embed.FS

func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7392206)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		body, readErr := migrationFiles.ReadFile("migrations/" + name)
		if readErr != nil {
			return readErr
		}
		// Migrations here contain plain SQL statements (no PL/pgSQL dollar quoting).
		for _, statement := range strings.Split(string(body), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migration %s: %w", name, err)
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, name); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func newToken() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	h := sha256.Sum256([]byte(token))
	return token, h[:], nil
}

// Invite generates a one-time registration invitation bound to an exact username.
// No plaintext invitation is stored in PostgreSQL; show the returned token once.
func (s *Store) Invite(ctx context.Context, username string) (string, error) {
	if !protocol.ValidUsername(username) {
		return "", errors.New("username must use 1-24 letters, digits or underscores")
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username_key=$1)`, strings.ToLower(username)).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return "", ErrUsernameUnavailable
	}
	raw, hash, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO invites(username_key,token_hash) VALUES($1,$2)`, strings.ToLower(username), hash)
	if err != nil {
		return "", err
	}
	return raw, nil
}

// Argon2id PHC-style stored hash; never persist plaintext passwords.
const hashMemory uint32 = 64 * 1024
const hashIterations uint32 = 3
const hashThreads uint8 = 2

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 128 {
		return "", errors.New("password must be 12-128 bytes")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := argon2.IDKey([]byte(password), salt, hashIterations, hashMemory, hashThreads, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", hashMemory, hashIterations, hashThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(derived)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory, iterations uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &threads); err != nil {
		return false
	}
	// Enforce expected parameters instead of trusting unbounded settings from DB.
	if memory != hashMemory || iterations != hashIterations || threads != hashThreads {
		return false
	}
	salt, a := base64.RawStdEncoding.DecodeString(parts[4])
	want, b := base64.RawStdEncoding.DecodeString(parts[5])
	if a != nil || b != nil || len(salt) != 16 || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, threads, uint32(len(want)))
	if len(got) != len(want) {
		return false
	}
	// Constant-time comparison for derived keys.
	return subtle.ConstantTimeCompare(got, want) == 1
}

func (s *Store) Register(ctx context.Context, username, invite, password, label string) (string, []string, error) {
	if !protocol.ValidUsername(username) || len(invite) != 43 {
		return "", nil, ErrInvalidInvite
	}
	digest := sha256.Sum256([]byte(invite))
	pwdHash, err := HashPassword(password)
	if err != nil {
		return "", nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	var inviteID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM invites WHERE username_key=$1 AND token_hash=$2 AND used_at IS NULL FOR UPDATE`, strings.ToLower(username), digest[:]).Scan(&inviteID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrInvalidInvite
	}
	if err != nil {
		return "", nil, err
	}
	var existing bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username_key=$1)`, strings.ToLower(username)).Scan(&existing)
	if err != nil {
		return "", nil, err
	}
	if existing {
		return "", nil, ErrUsernameUnavailable
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO users(username,username_key,password_hash) VALUES($1,$2,$3) RETURNING id`, username, strings.ToLower(username), pwdHash).Scan(&id)
	if err != nil {
		return "", nil, fmt.Errorf("register user (username may be taken): %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO conversation_members(conversation_id,user_id) VALUES(1,$1)`, id); err != nil {
		return "", nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE invites SET used_at=now() WHERE id=$1`, inviteID); err != nil {
		return "", nil, err
	}
	codes, err := generateRecoveryCodes(ctx, tx, id)
	if err != nil {
		return "", nil, err
	}
	token, err := createLabeledSession(ctx, tx, id, label)
	if err != nil {
		return "", nil, err
	}
	if err = tx.Commit(); err != nil {
		return "", nil, err
	}
	return token, codes, nil
}

func (s *Store) PasswordLogin(ctx context.Context, username, password string) (string, error) {
	return s.PasswordLoginDevice(ctx, username, password, "Legacy client")
}

func (s *Store) Authenticate(ctx context.Context, username, session string) (Identity, error) {
	if !protocol.ValidUsername(username) || len(session) != 43 {
		return Identity{}, ErrInvalidSession
	}
	digest := sha256.Sum256([]byte(session))
	var id Identity
	err := s.db.QueryRowContext(ctx, `SELECT users.id,users.username FROM sessions
        JOIN users ON users.id=sessions.user_id
        WHERE users.username_key=$1 AND sessions.token_hash=$2 AND sessions.expires_at>now()`,
		strings.ToLower(username), digest[:]).Scan(&id.ID, &id.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrInvalidSession
	}
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=now() WHERE token_hash=$1 AND last_seen_at<now()-interval '5 minutes'`, digest[:])
	}
	return id, err
}

// History includes the most recent 50 messages, plus all messages missed after
// the client's cursor when reconnecting. All current chat is in general room 1.
func (s *Store) History(ctx context.Context, since int64) ([]protocol.Message, int64, error) {
	var newest int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM messages WHERE conversation_id=1`).Scan(&newest)
	if err != nil {
		return nil, 0, err
	}
	var oldestRecent int64
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(id),0) FROM (
        SELECT id FROM messages WHERE conversation_id=1 ORDER BY id DESC LIMIT $1
        ) recent`, protocol.HistoryLimit).Scan(&oldestRecent)
	if err != nil {
		return nil, 0, err
	}
	after := oldestRecent - 1
	if since > 0 && since <= newest && since < after {
		after = since
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,COALESCE(u.username,m.legacy_username),m.body,m.created_at
        FROM messages m LEFT JOIN users u ON u.id=m.sender_id
        WHERE m.conversation_id=1 AND m.id>$1 ORDER BY m.id`, after)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []protocol.Message{}
	for rows.Next() {
		var m protocol.Message
		if err = rows.Scan(&m.ID, &m.Username, &m.Text, &m.SentAt); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, newest, nil
}

func (s *Store) InsertMessage(ctx context.Context, sender Identity, text string) (protocol.Message, error) {
	var m protocol.Message
	m.Username = sender.Username
	m.Text = text
	err := s.db.QueryRowContext(ctx, `INSERT INTO messages(conversation_id,sender_id,body)
        SELECT 1,$1,$2 WHERE EXISTS(
            SELECT 1 FROM conversation_members WHERE conversation_id=1 AND user_id=$1
        ) RETURNING id,created_at`, sender.ID, text).Scan(&m.ID, &m.SentAt)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.Message{}, errors.New("not a conversation member")
	}
	return m, err
}

// ImportJSON is an administrator-only, restart-time import. Existing v0.5
// message IDs are preserved for existing clients; old names are archival only.
func (s *Store) ImportJSON(ctx context.Context, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var history []protocol.Message
	if err = json.Unmarshal(data, &history); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7392206)`); err != nil {
		return 0, err
	}
	var live int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE sender_id IS NOT NULL`).Scan(&live); err != nil {
		return 0, err
	}
	if live > 0 {
		return 0, errors.New("import before sending new PostgreSQL messages; live messages already exist")
	}
	inserted := 0
	for _, m := range history {
		if m.ID <= 0 || !protocol.ValidUsername(m.Username) || len([]byte(m.Text)) < 1 || len([]byte(m.Text)) > protocol.MaxMessageBytes || m.SentAt.IsZero() {
			return 0, fmt.Errorf("invalid legacy record id=%d", m.ID)
		}
		result, e := tx.ExecContext(ctx, `INSERT INTO messages(id,conversation_id,legacy_username,body,created_at)
            VALUES($1,1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, m.ID, m.Username, m.Text, m.SentAt)
		if e != nil {
			return 0, e
		}
		n, _ := result.RowsAffected()
		inserted += int(n)
	}
	// Do not allow imported IDs to collide with subsequent auto-generated IDs.
	var seq int64
	err = tx.QueryRowContext(ctx, `SELECT setval(pg_get_serial_sequence('messages','id'),
        (SELECT COALESCE(MAX(id),1) FROM messages), EXISTS(SELECT 1 FROM messages))`).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return inserted, tx.Commit()
}
