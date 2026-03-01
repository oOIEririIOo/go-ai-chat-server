package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// jwtSecret 用于签名 JWT 的密钥，生产环境应从环境变量读取
	jwtSecret = []byte("your-secret-key-change-in-production")
	// TokenExpireDuration Token 有效期（30天）
	TokenExpireDuration = time.Hour * 24 * 30
)

// Claims 自定义 JWT Claims 结构
type Claims struct {
	UserID       uint   `json:"user_id"`
	Username     string `json:"username"`
	TokenVersion int    `json:"token_version"` // Token 版本号
	jwt.RegisteredClaims
}

// GenerateToken 生成 JWT Token
// 参数:
//   - userID: 用户ID
//   - username: 用户名
//   - tokenVersion: Token 版本号
//
// 返回:
//   - tokenString: 生成的 Token 字符串
//   - err: 错误信息
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
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return "", fmt.Errorf("生成 Token 失败: %w", err)
	}

	return tokenString, nil
}

// ParseToken 解析 JWT Token
// 参数:
//   - tokenString: Token 字符串
//
// 返回:
//   - claims: 解析后的 Claims
//   - err: 错误信息
func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外的签名方法: %v", token.Header["alg"])
		}
		return jwtSecret, nil
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

// RefreshToken 刷新 Token
// 参数:
//   - tokenString: 旧的 Token 字符串
//
// 返回:
//   - newTokenString: 新的 Token 字符串
//   - err: 错误信息
func RefreshToken(tokenString string) (string, error) {
	claims, err := ParseToken(tokenString)
	if err != nil {
		return "", err
	}

	// 生成新的 Token，包含当前 TokenVersion
	return GenerateToken(claims.UserID, claims.Username, claims.TokenVersion)
}
