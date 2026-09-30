package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
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

	user, token, err := h.authService.Register(req.FirstName, req.LastName, req.Email, req.Password)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusConflict, err.Error())
		return
	}
	cookie := &http.Cookie{
		Name:     "access_token",
		Value:    token,
		HttpOnly: true,
	}
	http.SetCookie(w, cookie)
	utils.WriteJSONResponse(w, http.StatusCreated, user)
}

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

	user, token, err := h.authService.Login(req.Email, req.Password)
	if err != nil {
		utils.WriteJSONResponse(w, http.StatusUnauthorized, err.Error())
		return
	}
	cookie := &http.Cookie{
		Name:     "access_token",
		Value:    token,
		HttpOnly: true,
	}
	http.SetCookie(w, cookie)
	utils.WriteJSONResponse(w, http.StatusOK, user)
}
