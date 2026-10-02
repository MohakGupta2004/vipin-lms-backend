package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
	"github.com/golang-jwt/jwt/v5"
)

type RegisterRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Password  string `json:"password"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthHandler struct {
	userRepo    *models.UserRepository
	authService *service.AuthService
}

func NewAuthHandler(userRepo *models.UserRepository, authService *service.AuthService) *AuthHandler {
	return &AuthHandler{
		userRepo:    userRepo,
		authService: authService,
	}
}

// RegisterHandler godoc
//
//	@Summary		Register a new user
//	@Description	Creates a student account and sets access_token and refresh_token as HttpOnly cookies.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		RegisterRequest							true	"Registration details"
//	@Success		201		{object}	utils.JSONResponse{data=models.User}	"User created"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or missing required fields"
//	@Failure		409		{object}	utils.JSONResponse						"User already exists"
//	@Router			/auth/register [post]
func (h *AuthHandler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Malformed payload", http.StatusBadRequest)
		return
	}

	if req.FirstName == "" || req.LastName == "" || req.Email == "" || req.Password == "" {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "Missing required fields")
		return
	}

	user, token, refresh, err := h.authService.Register(req.FirstName, req.LastName, req.Email, req.Password)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusConflict, err.Error())
		return
	}
	setAuthCookies(w, token, refresh)
	utils.WriteJSONResponse(w, http.StatusCreated, user)
}

// LoginHandler godoc
//
//	@Summary		Log in
//	@Description	Authenticates a user and sets access_token and refresh_token as HttpOnly cookies.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			request	body		LoginRequest							true	"Login credentials"
//	@Success		200		{object}	utils.JSONResponse{data=models.User}	"Logged in"
//	@Failure		400		{object}	utils.JSONResponse						"Malformed payload or missing required fields"
//	@Failure		401		{object}	utils.JSONResponse						"Invalid credentials"
//	@Router			/auth/login [post]
func (h *AuthHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	// Implement login logic here
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Malformed payload", http.StatusBadRequest)
		return
	}

	if req.Email == "" || req.Password == "" {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "Missing required fields")
		return
	}

	user, token, refresh, err := h.authService.Login(req.Email, req.Password)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, err.Error())
		return
	}
	setAuthCookies(w, token, refresh)
	utils.WriteJSONResponse(w, http.StatusOK, user)
}

// RefreshTokenHandler godoc
//
//	@Summary		Refresh tokens
//	@Description	Reads the refresh_token cookie and issues new access_token and refresh_token cookies.
//	@Description	The refresh_token cookie is sent automatically by the browser after login/register.
//	@Tags			auth
//	@Produce		json
//	@Success		202	{object}	utils.JSONResponse{data=string}	"Tokens refreshed"
//	@Failure		400	{object}	utils.JSONResponse				"Missing/invalid refresh token or user not found"
//	@Failure		500	{object}	utils.JSONResponse				"Failed to generate new tokens"
//	@Router			/auth/refresh [post]
func (h *AuthHandler) RefreshTokenHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tokenCookie, err := r.Cookie("refresh_token")
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "missing refresh token")
		return
	}

	if tokenCookie.Value == "" {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "missing refresh token")
		return
	}

	token, err := h.authService.ValidateRefreshToken(tokenCookie.Value)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	claims := token.Claims.(jwt.MapClaims)
	userId := claims["sub"].(string)

	user, err := h.userRepo.GetUserById(userId, ctx)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusBadRequest, "user not found")
		return
	}
	newAccessToken, newRefreshToken, err := h.authService.GenerateTokens(user)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusInternalServerError, "failed to generate new tokens")
		return
	}
	setAuthCookies(w, newAccessToken, newRefreshToken)
	utils.WriteJSONResponse(w, http.StatusAccepted, "access token updated successfully")
}

// setAuthCookies stores both tokens as HttpOnly cookies.
// Path "/" makes the browser send them to every API route, not only /auth.
func setAuthCookies(w http.ResponseWriter, accessToken, refreshToken string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    accessToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
