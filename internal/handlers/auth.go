package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
)

type RegisterRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Password  string `json:"password"`
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

type RegisterResponse struct {
	User  *models.User `json:"user"`
	Token string       `json:"token"`
}

func (h *AuthHandler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	user, token, err := h.authService.Register(req.FirstName, req.LastName, req.Email, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := RegisterResponse{
		User:  user,
		Token: token,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
