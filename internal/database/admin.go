package database

import (
	"context"
	"errors"
	"strings"

	"sockt/internal/protocol"
)

func (s *Store) AccountCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count)
	return count, err
}

// RevokeInvites removes only unused invitations for this username. DELETE takes
// the same row lock as Register's SELECT FOR UPDATE. If registration wins, its
// used_at update is rechecked and the consumed invitation is retained; if DELETE
// wins, registration can no longer find the invitation. No secret is needed.
func (s *Store) RevokeInvites(ctx context.Context, username string) (int64, error) {
	if !protocol.ValidUsername(username) {
		return 0, errors.New("invalid username")
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM invites WHERE username_key=$1 AND used_at IS NULL`, strings.ToLower(username))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
