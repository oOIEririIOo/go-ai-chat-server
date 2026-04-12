package service

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecretFallback   = []byte("your-secret-key-change-in-production")
	TokenExpireDuration = time.Hour * 24 * 30
)

func currentJWTSecret() []byte {
	if secret := strings.TrimSpace(os.Getenv("JWT_SECRET")); secret != "" {
		return []byte(secret)
	}
	return jwtSecretFallback
}

type Claims struct {
	UserID       uint   `json:"user_id"`
	Username     string `json:"username"`
	TokenVersion int    `json:"token_version"`
	jwt.RegisteredClaims
}

func GenerateToken(userID uint, username string, tokenVersion int) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:       userID,
		Username:     username,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(TokenExpireDuration)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "ai-chat-backend",
			Subject:   fmt.Sprintf("%d", userID),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(currentJWTSecret())
	if err != nil {
		return "", fmt.Errorf("生成 Token 失败: %w", err)
	}

	return tokenString, nil
}

func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外的签名方法: %v", token.Header["alg"])
		}
		return currentJWTSecret(), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errors.New("Token 已过期")
		}
		return nil, fmt.Errorf("解析 Token 失败: %w", err)
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("无效的 Token")
}

func RefreshToken(tokenString string) (string, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return "", err
	}

	return GenerateToken(claims.UserID, claims.Username, claims.TokenVersion)
}
