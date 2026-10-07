package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/email"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/middleware"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

const maxVerificationBody = 4 << 10 // these payloads are tiny; refuse anything bigger

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type VerifyOTPRequest struct {
	Email   string `json:"email"`
	Purpose string `json:"purpose" enums:"email_verification,password_reset"`
	OTP     string `json:"otp"`
}

type VerifyOTPResponse struct {
	// ResetToken is returned only for purpose password_reset; send it to /auth/reset-password.
	ResetToken string `json:"resetToken,omitempty"`
}

type ResetPasswordRequest struct {
	ResetToken  string `json:"resetToken"`
	NewPassword string `json:"newPassword"`
}

type VerificationHandler struct {
	svc *service.VerificationService
}

func NewVerificationHandler(svc *service.VerificationService) *VerificationHandler {
	return &VerificationHandler{svc: svc}
}

// SendVerificationEmail godoc
//
//	@Summary		Send an email verification code
//	@Description	Emails a 6-digit code to the logged-in user's address. The email is sent asynchronously. One code per minute; the code is valid for 10 minutes.
//	@Tags			auth
//	@Produce		json
//	@Success		202	{object}	utils.JSONResponse{data=string}	"Code queued for delivery"
//	@Failure		401	{object}	utils.JSONResponse				"Not logged in"
//	@Failure		409	{object}	utils.JSONResponse				"Email already verified"
//	@Failure		429	{object}	utils.JSONResponse				"Too many requests"
//	@Failure		503	{object}	utils.JSONResponse				"Email queue busy, try again shortly"
//	@Router			/auth/verify-email [post]
func (h *VerificationHandler) SendVerificationEmail(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
		return
	}
	if err := h.svc.SendVerification(r.Context(), user); err != nil {
		h.writeError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusAccepted, "verification code sent")
}

// ForgotPassword godoc
//
//	@Summary		Request a password reset code
//	@Description	Emails a 6-digit reset code if the address belongs to an account. The response is identical either way, so it does not reveal which addresses are registered.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		ForgotPasswordRequest				true	"Account email"
//	@Success		202		{object}	utils.JSONResponse{data=string}	"Accepted"
//	@Failure		400		{object}	utils.JSONResponse					"Malformed payload or invalid email"
//	@Failure		429		{object}	utils.JSONResponse					"Too many requests"
//	@Router			/auth/forgot-password [post]
func (h *VerificationHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	address, ok := normalizeEmail(req.Email)
	if !ok {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "invalid email")
		return
	}
	if err := h.svc.RequestPasswordReset(r.Context(), address); err != nil {
		slog.Error("password reset request failed", "err", err)
		// same response as success: an error must not distinguish existing accounts
	}
	utils.WriteJSONResponse(w, http.StatusAccepted, "if the email is registered, a reset code has been sent")
}

// VerifyOTP godoc
//
//	@Summary		Verify a one-time code
//	@Description	purpose=email_verification marks the email verified. purpose=password_reset returns a single-use resetToken (valid 10 minutes) for /auth/reset-password. A code allows 5 wrong guesses.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		VerifyOTPRequest								true	"Email, purpose and code"
//	@Success		200		{object}	utils.JSONResponse{data=VerifyOTPResponse}	"Code accepted"
//	@Failure		400		{object}	utils.JSONResponse								"Malformed payload, or wrong, expired or used code"
//	@Failure		429		{object}	utils.JSONResponse								"Too many requests"
//	@Router			/auth/verify-otp [post]
func (h *VerificationHandler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req VerifyOTPRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	purpose := models.OTPPurpose(req.Purpose)
	address, ok := normalizeEmail(req.Email)
	if !ok || !purpose.Valid() || req.OTP == "" {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "email, purpose and otp are required")
		return
	}
	resetToken, err := h.svc.VerifyOTP(r.Context(), address, purpose, req.OTP)
	if err != nil {
		h.writeError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, VerifyOTPResponse{ResetToken: resetToken})
}

// ResetPassword godoc
//
//	@Summary		Set a new password
//	@Description	Spends the resetToken from /auth/verify-otp. The password must be 8 to 72 characters. A confirmation email is sent.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		ResetPasswordRequest				true	"Reset token and new password"
//	@Success		200		{object}	utils.JSONResponse{data=string}	"Password changed"
//	@Failure		400		{object}	utils.JSONResponse					"Malformed payload, weak password, or invalid/expired reset token"
//	@Failure		429		{object}	utils.JSONResponse					"Too many requests"
//	@Router			/auth/reset-password [post]
func (h *VerificationHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if !decodeSmallJSON(w, r, &req) {
		return
	}
	if err := h.svc.ResetPassword(r.Context(), req.ResetToken, req.NewPassword); err != nil {
		h.writeError(w, err)
		return
	}
	utils.WriteJSONResponse(w, http.StatusOK, "password updated")
}

func (h *VerificationHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrAlreadyVerified):
		utils.WriteJSONResponse(w, http.StatusConflict, err.Error())
	case errors.Is(err, models.ErrOTPCooldown):
		w.Header().Set("Retry-After", "60")
		utils.WriteJSONResponse(w, http.StatusTooManyRequests, "a code was sent recently, wait a minute before requesting another")
	case errors.Is(err, email.ErrQueueFull), errors.Is(err, email.ErrQueueClosed):
		utils.WriteJSONResponse(w, http.StatusServiceUnavailable, "email service is busy, try again shortly")
	case errors.Is(err, models.ErrOTPInvalid), errors.Is(err, models.ErrResetTokenInvalid), errors.Is(err, service.ErrWeakPassword):
		utils.WriteJSONResponse(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("verification request failed", "err", err)
		utils.WriteJSONResponse(w, http.StatusInternalServerError, "internal server error")
	}
}

// decodeSmallJSON reads a size-capped JSON body and writes the 400 itself on failure.
func decodeSmallJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxVerificationBody)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "malformed payload")
		return false
	}
	return true
}

// normalizeEmail trims and lowercases an address and checks it is a plain address (no display name).
func normalizeEmail(raw string) (string, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" || len(raw) > 254 {
		return "", false
	}
	parsed, err := mail.ParseAddress(raw)
	if err != nil || parsed.Address != raw {
		return "", false
	}
	return raw, true
}
