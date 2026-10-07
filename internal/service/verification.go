package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"
	"unicode/utf8"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/email"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

const (
	otpLength        = 6
	otpTTL           = 10 * time.Minute
	otpCooldown      = time.Minute // minimum gap between codes for one user and purpose
	resetTokenTTL    = 10 * time.Minute
	minPasswordBytes = 8
	maxPasswordBytes = 72 // bcrypt ignores or rejects anything longer
)

var (
	ErrAlreadyVerified = errors.New("email is already verified")
	ErrWeakPassword    = fmt.Errorf("password must be %d to %d characters", minPasswordBytes, maxPasswordBytes)
)

// EmailEnqueuer queues a rendered email for asynchronous delivery. Implemented by *email.Queue.
type EmailEnqueuer interface {
	Enqueue(msg email.Message) error
}

var _ EmailEnqueuer = (*email.Queue)(nil)

// VerificationService handles email verification and password reset with one-time codes.
type VerificationService struct {
	userRepo *models.UserRepository
	otpRepo  *models.OTPRepository
	mailer   EmailEnqueuer
	auth     *AuthService
	hmacKey  []byte
}

func NewVerificationService(userRepo *models.UserRepository, otpRepo *models.OTPRepository, mailer EmailEnqueuer, auth *AuthService, hmacKey string) *VerificationService {
	return &VerificationService{userRepo: userRepo, otpRepo: otpRepo, mailer: mailer, auth: auth, hmacKey: []byte(hmacKey)}
}

// SendVerification emails a verification code to a logged-in user.
func (s *VerificationService) SendVerification(ctx context.Context, user *models.User) error {
	if user.EmailVerified {
		return ErrAlreadyVerified
	}
	return s.issueAndSend(ctx, user, models.PurposeEmailVerification, email.KindVerifyEmail)
}

// RequestPasswordReset emails a reset code when the address belongs to an account.
// It returns nil for unknown addresses and for rate-limited or undeliverable requests,
// so the response never reveals whether an account exists.
func (s *VerificationService) RequestPasswordReset(ctx context.Context, address string) error {
	user, err := s.userRepo.GetUserByEmail(address, ctx)
	if err != nil {
		return err
	}
	if user == nil {
		return nil
	}
	err = s.issueAndSend(ctx, user, models.PurposePasswordReset, email.KindPasswordReset)
	if errors.Is(err, models.ErrOTPCooldown) || errors.Is(err, email.ErrQueueFull) {
		slog.Warn("password reset email not sent", "reason", err)
		return nil
	}
	return err
}

// VerifyOTP checks a code. For email verification it marks the email verified and returns "".
// For password reset it returns a single-use reset token to pass to ResetPassword.
func (s *VerificationService) VerifyOTP(ctx context.Context, address string, purpose models.OTPPurpose, code string) (string, error) {
	user, err := s.userRepo.GetUserByEmail(address, ctx)
	if err != nil {
		return "", err
	}
	if user == nil || !validCode(code) {
		return "", models.ErrOTPInvalid
	}

	var resetToken, resetHash string
	if purpose == models.PurposePasswordReset {
		resetToken, err = randomToken()
		if err != nil {
			return "", err
		}
		resetHash = hashToken(resetToken)
	}

	want := s.hashCode(user.ID, purpose, code)
	matches := func(stored string) bool { return hmac.Equal([]byte(stored), []byte(want)) }
	if err := s.otpRepo.Verify(ctx, user.ID, purpose, matches, resetHash, resetTokenTTL); err != nil {
		return "", err
	}

	if purpose == models.PurposeEmailVerification {
		s.notify(user, email.KindEmailVerified, email.Data{FirstName: user.FirstName})
	}
	return resetToken, nil
}

// ResetPassword spends a reset token and sets the new password.
func (s *VerificationService) ResetPassword(ctx context.Context, resetToken, newPassword string) error {
	if n := len(newPassword); n < minPasswordBytes || n > maxPasswordBytes || !utf8.ValidString(newPassword) {
		return ErrWeakPassword
	}
	if resetToken == "" {
		return models.ErrResetTokenInvalid
	}
	hash, err := s.auth.HashPassword(newPassword)
	if err != nil {
		return err
	}
	user, err := s.otpRepo.ResetPassword(ctx, hashToken(resetToken), hash)
	if err != nil {
		return err
	}
	s.notify(user, email.KindPasswordChanged, email.Data{FirstName: user.FirstName})
	return nil
}

// issueAndSend stores a fresh code and queues the email carrying it.
func (s *VerificationService) issueAndSend(ctx context.Context, user *models.User, purpose models.OTPPurpose, kind email.Kind) error {
	code, err := randomCode()
	if err != nil {
		return err
	}
	if err := s.otpRepo.Issue(ctx, user.ID, purpose, s.hashCode(user.ID, purpose, code), otpTTL, otpCooldown); err != nil {
		return err
	}
	msg, err := email.Render(kind, user.Email, email.Data{
		FirstName:     user.FirstName,
		Code:          code,
		ExpiresInMins: int(otpTTL.Minutes()),
	})
	if err != nil {
		return err
	}
	return s.mailer.Enqueue(msg)
}

// notify queues a best-effort email with no code; a failure is logged, not returned.
func (s *VerificationService) notify(user *models.User, kind email.Kind, data email.Data) {
	msg, err := email.Render(kind, user.Email, data)
	if err == nil {
		err = s.mailer.Enqueue(msg)
	}
	if err != nil {
		slog.Warn("notification email not queued", "kind", kind, "err", err)
	}
}

// hashCode binds a code to its user and purpose with a keyed hash, so a leaked table cannot be brute-forced offline
// and a code for one purpose cannot be replayed for another.
func (s *VerificationService) hashCode(userID string, purpose models.OTPPurpose, code string) string {
	mac := hmac.New(sha256.New, s.hmacKey)
	mac.Write([]byte(string(purpose) + ":" + userID + ":" + code))
	return hex.EncodeToString(mac.Sum(nil))
}

func validCode(code string) bool {
	if len(code) != otpLength {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func randomCode() (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(otpLength), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", otpLength, n), nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken hashes a high-entropy token for storage; a plain SHA-256 is enough because it cannot be guessed.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
