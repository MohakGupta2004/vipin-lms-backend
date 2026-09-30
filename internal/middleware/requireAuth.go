package middleware

import (
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
	"github.com/golang-jwt/jwt/v5"
)

type AuthMiddleware struct {
	SecretKey   string
	AuthService *service.AuthService
	UserRepo    *models.UserRepository
}

func NewAuthMiddleware(secretKey string, authService *service.AuthService, userRepo *models.UserRepository) *AuthMiddleware {
	return &AuthMiddleware{
		SecretKey:   secretKey,
		AuthService: authService,
		UserRepo:    userRepo,
	}
}

func (am *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if the user is authenticated (e.g., check for a valid session or token)
		ctx := r.Context()
		cookie, err := r.Cookie("access_token")
		if err != nil || cookie.Value == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// If not authenticated, return an error response
		token, err := am.AuthService.ValidateToken(cookie.Value)
		if err != nil || !token.Valid {
			utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
			return
		}
		claims := token.Claims.(jwt.MapClaims)
		userId := claims["sub"].(string)

		_, err = am.UserRepo.GetUserById(userId, ctx)
		if err != nil {
			utils.WriteJSONResponse(w, http.StatusUnauthorized, "username doesn't exists")
		}
		next.ServeHTTP(w, r)
	})
}
