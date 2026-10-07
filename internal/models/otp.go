package models

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type OTPPurpose string

const (
	PurposeEmailVerification OTPPurpose = "email_verification"
	PurposePasswordReset     OTPPurpose = "password_reset"
)

// Valid reports whether p is a known purpose.
func (p OTPPurpose) Valid() bool {
	return p == PurposeEmailVerification || p == PurposePasswordReset
}

var (
	// ErrOTPCooldown means a code for this user and purpose was issued too recently.
	ErrOTPCooldown = errors.New("a code was sent recently")
	// ErrOTPInvalid covers a wrong, expired, used or locked-out code. It deliberately does not say which.
	ErrOTPInvalid = errors.New("invalid or expired code")
	// ErrResetTokenInvalid covers an unknown, expired or used reset token.
	ErrResetTokenInvalid = errors.New("invalid or expired reset token")
)

// MaxOTPAttempts is how many wrong guesses a code survives.
const MaxOTPAttempts = 5

type OTPRepository struct {
	db *sql.DB
}

func NewOTPRepository(db *sql.DB) *OTPRepository {
	return &OTPRepository{db: db}
}

// Issue replaces any open code for the user and purpose with a new one.
// It returns ErrOTPCooldown when the latest code is younger than cooldown.
// The user row lock serialises concurrent requests for the same user.
func (r *OTPRepository) Issue(ctx context.Context, userID string, purpose OTPPurpose, codeHash string, ttl, cooldown time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return err
	}

	var recent bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM otp_codes
		WHERE user_id = $1 AND purpose = $2 AND created_at > now() - make_interval(secs => $3))`,
		userID, purpose, cooldown.Seconds()).Scan(&recent)
	if err != nil {
		return err
	}
	if recent {
		return ErrOTPCooldown
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM otp_codes WHERE user_id = $1 AND purpose = $2`, userID, purpose); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO otp_codes (user_id, purpose, code_hash, expires_at)
		VALUES ($1, $2, $3, now() + make_interval(secs => $4))`,
		userID, purpose, codeHash, ttl.Seconds())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Verify checks a submitted code against the user's open code for the purpose.
// matches receives the stored hash. A wrong code counts an attempt; after MaxOTPAttempts the code is dead.
//
// Email verification: a match consumes the code and marks the user's email verified; resetTokenHash is ignored.
// Password reset: a match stores resetTokenHash (valid for resetTTL) and leaves the code open until ResetPassword.
func (r *OTPRepository) Verify(ctx context.Context, userID string, purpose OTPPurpose, matches func(codeHash string) bool, resetTokenHash string, resetTTL time.Duration) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id, codeHash string
	err = tx.QueryRowContext(ctx, `SELECT id, code_hash FROM otp_codes
		WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL AND verified_at IS NULL
		  AND expires_at > now() AND attempts < $3
		ORDER BY created_at DESC LIMIT 1 FOR UPDATE`,
		userID, purpose, MaxOTPAttempts).Scan(&id, &codeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrOTPInvalid
	}
	if err != nil {
		return err
	}

	if !matches(codeHash) {
		if _, err := tx.ExecContext(ctx, `UPDATE otp_codes SET attempts = attempts + 1 WHERE id = $1`, id); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil { // keep the attempt count even though the verification failed
			return err
		}
		return ErrOTPInvalid
	}

	switch purpose {
	case PurposeEmailVerification:
		if _, err := tx.ExecContext(ctx, `UPDATE otp_codes SET consumed_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = $1`, userID); err != nil {
			return err
		}
	case PurposePasswordReset:
		if _, err := tx.ExecContext(ctx, `UPDATE otp_codes
			SET verified_at = now(), reset_token_hash = $2, reset_expires_at = now() + make_interval(secs => $3)
			WHERE id = $1`, id, resetTokenHash, resetTTL.Seconds()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ResetPassword spends a reset token: it sets the new password hash, clears the stored refresh token
// and drops all of the user's reset codes. It returns the user.
func (r *OTPRepository) ResetPassword(ctx context.Context, resetTokenHash, newPasswordHash string) (*User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id, userID string
	err = tx.QueryRowContext(ctx, `SELECT id, user_id FROM otp_codes
		WHERE reset_token_hash = $1 AND purpose = 'password_reset'
		  AND consumed_at IS NULL AND reset_expires_at > now()
		FOR UPDATE`, resetTokenHash).Scan(&id, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrResetTokenInvalid
	}
	if err != nil {
		return nil, err
	}

	user := &User{}
	err = tx.QueryRowContext(ctx, `UPDATE users SET password = $2, refresh_token = NULL WHERE id = $1
		RETURNING id, first_name, last_name, email, role`, userID, newPasswordHash).
		Scan(&user.ID, &user.FirstName, &user.LastName, &user.Email, &user.Role)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM otp_codes WHERE user_id = $1 AND purpose = 'password_reset'`, userID); err != nil {
		return nil, err
	}
	return user, tx.Commit()
}
