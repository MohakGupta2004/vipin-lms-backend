package middleware

import (
	"context"
	"net/http"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/lib/utils"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/MohakGupta2004/vipin-lms-backend/internal/service"
	"github.com/golang-jwt/jwt/v5"
)

// contextKey is a private type so no other package can overwrite our context values.
type contextKey string

const userContextKey contextKey = "user"

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

// RequireAuth checks the access_token cookie, loads the user from the database
// and stores it in the request context. Handlers read it with UserFromContext.
func (am *AuthMiddleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("access_token")
		if err != nil || cookie.Value == "" {
			utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
			return
		}

		token, err := am.AuthService.ValidateToken(cookie.Value)
		if err != nil || !token.Valid {
			utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
			return
		}
		userID, err := claims.GetSubject()
		if err != nil || userID == "" {
			utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
			return
		}

		// Load the user fresh from the database so a changed role or a
		// deleted account takes effect right away, not when the token expires.
		user, err := am.UserRepo.GetUserById(userID, r.Context())
		if err != nil || user == nil {
			utils.WriteJSONResponse(w, http.StatusUnauthorized, "unauthorized access")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFromContext returns the logged-in user stored by RequireAuth.
func UserFromContext(ctx context.Context) (*models.User, bool) {
	user, ok := ctx.Value(userContextKey).(*models.User)
	return user, ok && user != nil
}
