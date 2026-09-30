package service

import (
	"context"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrUserAlreadyExists = errors.New("user already exists")
)

type AuthService struct {
	// Add any dependencies or configurations needed for the AuthService
	jwtSecretKey       string
	refreshSecretKey   string
	accessTokenExpiry  time.Duration
	userRepository     *models.UserRepository // Assuming you have a UserRepository interface for database operations
	databaseContext    context.Context        // Assuming you have a context for database operations
	refreshTokenExpiry time.Duration
}

// NewAuthService creates a new instance of AuthService
func NewAuthService(userRepository *models.UserRepository, databaseContext context.Context, jwtSecretKey string, accessTokenExpiry time.Duration, refreshSecretKey string, refreshTokenExpiry time.Duration) *AuthService {
	return &AuthService{
		userRepository:     userRepository,
		databaseContext:    databaseContext,
		jwtSecretKey:       jwtSecretKey,       // Assuming you have a secret key for JWT signing
		accessTokenExpiry:  accessTokenExpiry,  // Set the access token expiry duration
		refreshSecretKey:   refreshSecretKey,   // Assuming you have a secret key for refresh token signing
		refreshTokenExpiry: refreshTokenExpiry, // Set the refresh token expiry duration
	}
}

func (s *AuthService) Register(firstName, lastName, email, password string) (*models.User, string, string, error) {
	user, err := s.userRepository.GetUserByEmail(email, s.databaseContext)
	if err != nil {
		return nil, "", "", err
	}
	if user != nil {
		return nil, "", "", ErrUserAlreadyExists // Assuming you have a custom error for user already exists
	}

	hashedPassword, err := s.HashPassword(password)
	if err != nil {
		return nil, "", "", err
	}

	registeredUser, err := s.userRepository.CreateUser(firstName, lastName, email, hashedPassword, s.databaseContext)
	if err != nil {
		return nil, "", "", err
	}

	token, refresh, err := s.GenerateTokens(registeredUser)
	if err != nil {
		return nil, "", "", err
	}
	return registeredUser, token, refresh, nil
}

func (s *AuthService) Login(email, password string) (*models.User, string, string, error) {
	user, err := s.userRepository.GetUserByEmail(email, s.databaseContext)
	if err != nil {
		return nil, "", "", err
	}
	if user == nil {
		return nil, "", "", errors.New("user not found")
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, "", "", errors.New("invalid password")
	}

	token, refresh, err := s.GenerateTokens(user)
	if err != nil {
		return nil, "", "", err
	}
	return user, token, refresh, nil
}

func (s *AuthService) HashPassword(password string) (string, error) {
	// Implement password hashing logic here (e.g., using bcrypt)
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedPassword), nil
}

func (s *AuthService) GenerateTokens(user *models.User) (string, string, error) {
	expirationTime := time.Now().Add(s.accessTokenExpiry) // Use the configured access token expiry
	claims := jwt.MapClaims{
		"sub":      user.ID,
		"username": user.Email,
		"exp":      expirationTime.Unix(),
		"iat":      time.Now().Unix(),
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessTokenString, err := accessToken.SignedString([]byte(s.jwtSecretKey))
	if err != nil {
		return "", "", err
	}

	expirationTime = time.Now().Add(s.refreshTokenExpiry)
	refreshClaims := jwt.MapClaims{
		"sub":      user.ID,
		"username": user.Email,
		"exp":      expirationTime.Unix(),
		"iat":      time.Now().Unix(),
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refrshTokenString, err := refreshToken.SignedString([]byte(s.refreshSecretKey))

	if err != nil {
		return "", "", err
	}
	_, err = s.userRepository.UpdateRefreshToken(user.ID, refrshTokenString, s.databaseContext)
	if err != nil {
		return "", "", err
	}
	return accessTokenString, refrshTokenString, nil
}

func (s *AuthService) ValidateToken(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.jwtSecretKey), nil
	})
	if err != nil {
		return nil, err
	}

	return token, nil
}

func (s *AuthService) ValidateRefreshToken(tokenString string) (*jwt.Token, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.refreshSecretKey), nil
	})
	if err != nil {
		return nil, err
	}

	return token, nil
}
