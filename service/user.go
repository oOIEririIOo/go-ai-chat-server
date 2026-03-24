package service

import (
	"ai-chat/models"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func RegisterUser(db *gorm.DB, user *models.User) (*models.User, error) {
	fmt.Printf("[AuthDebug] Service: 开始注册用户，用户名=%s\n", user.Username)

	// 检查用户是否已存在
	var existingUser models.User
	if db.Where("username = ?", user.Username).First(&existingUser).Error == nil {
		fmt.Printf("[AuthDebug] Service: 用户已存在，用户名=%s\n", user.Username)
		return nil, errors.New("用户已存在")
	}

	// 密码加密
	fmt.Printf("[AuthDebug] Service: 正在加密密码\n")
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("[AuthDebug] Service: 密码加密失败: %v\n", err)
		return nil, err
	}
	user.Password = string(hashedPassword)

	// 创建用户
	fmt.Printf("[AuthDebug] Service: 正在创建用户到数据库\n")
	if result := db.Create(user); result.Error != nil {
		fmt.Printf("[AuthDebug] Service: 数据库创建用户失败: %v\n", result.Error)
		return nil, result.Error
	}

	fmt.Printf("[AuthDebug] Service: 用户注册成功，ID=%d\n", user.ID)
	return user, nil
}

func LoginUser(db *gorm.DB, username, password string) (*models.User, error) {
	fmt.Printf("[AuthDebug] Service: 开始登录验证，用户名=%s\n", username)

	var user models.User
	// 查找用户
	fmt.Printf("[AuthDebug] Service: 正在查询用户\n")
	if result := db.Where("username = ?", username).First(&user); result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			fmt.Printf("[AuthDebug] Service: 用户不存在，用户名=%s\n", username)
			return nil, errors.New("用户不存在")
		}
		fmt.Printf("[AuthDebug] Service: 数据库查询失败: %v\n", result.Error)
		return nil, result.Error
	}

	// 验证密码
	fmt.Printf("[AuthDebug] Service: 正在验证密码\n")
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		fmt.Printf("[AuthDebug] Service: 密码验证失败\n")
		return nil, errors.New("密码不正确")
	}

	fmt.Printf("[AuthDebug] Service: 登录验证成功，用户ID=%d\n", user.ID)
	return &user, nil
}

// ChangePasswordRequest 修改密码请求结构
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// ChangePassword 修改密码
// 功能: 验证旧密码，更新为新密码，并使所有旧 Token 失效
// 参数:
//   - db: 数据库连接
//   - userID: 用户ID
//   - oldPassword: 旧密码
//   - newPassword: 新密码
//
// 返回:
//   - error: 错误信息
func ChangePassword(db *gorm.DB, userID uint, oldPassword, newPassword string) error {
	fmt.Printf("[AuthDebug] Service: 开始修改密码，用户ID=%d\n", userID)

	var user models.User
	// 查找用户
	if result := db.First(&user, userID); result.Error != nil {
		fmt.Printf("[AuthDebug] Service: 用户不存在，ID=%d\n", userID)
		return errors.New("用户不存在")
	}

	// 验证旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		fmt.Printf("[AuthDebug] Service: 旧密码验证失败\n")
		return errors.New("旧密码不正确")
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		fmt.Printf("[AuthDebug] Service: 新密码加密失败: %v\n", err)
		return err
	}

	// 更新密码并递增 TokenVersion（使所有旧 Token 失效）
	result := db.Model(&user).Updates(map[string]interface{}{
		"password":      string(hashedPassword),
		"token_version": gorm.Expr("token_version + 1"),
	})
	if result.Error != nil {
		fmt.Printf("[AuthDebug] Service: 更新密码失败: %v\n", result.Error)
		return result.Error
	}

	fmt.Printf("[AuthDebug] Service: 密码修改成功，TokenVersion 已递增，用户ID=%d\n", userID)
	return nil
}

// ChangeUsername 修改登录账号。
// username 当前即登录账号字段，不是独立昵称字段。
func ChangeUsername(db *gorm.DB, userID uint, newUsername string) (*models.User, error) {
	fmt.Printf("[AuthDebug] Service: 开始修改用户名，用户ID=%d\n", userID)

	trimmedUsername := strings.TrimSpace(newUsername)
	if trimmedUsername == "" {
		return nil, errors.New("用户名不能为空")
	}

	var user models.User
	if result := db.First(&user, userID); result.Error != nil {
		fmt.Printf("[AuthDebug] Service: 用户不存在，ID=%d\n", userID)
		return nil, errors.New("用户不存在")
	}

	if user.Username == trimmedUsername {
		return nil, errors.New("新用户名不能与当前用户名相同")
	}

	var existingUser models.User
	if result := db.Where("username = ?", trimmedUsername).First(&existingUser); result.Error == nil {
		fmt.Printf("[AuthDebug] Service: 用户名已存在，用户名=%s\n", trimmedUsername)
		return nil, errors.New("用户名已存在")
	} else if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		fmt.Printf("[AuthDebug] Service: 查询用户名唯一性失败: %v\n", result.Error)
		return nil, result.Error
	}

	if result := db.Model(&user).Update("username", trimmedUsername); result.Error != nil {
		fmt.Printf("[AuthDebug] Service: 更新用户名失败: %v\n", result.Error)
		return nil, result.Error
	}

	user.Username = trimmedUsername
	fmt.Printf("[AuthDebug] Service: 用户名修改成功，用户ID=%d，新用户名=%s\n", userID, user.Username)
	return &user, nil
}
