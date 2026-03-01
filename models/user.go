package models

import (
	"gorm.io/gorm"
)

// User 用户表
type User struct {
	gorm.Model
	Username     string `gorm:"unique;not null" json:"username"`
	Password     string `gorm:"not null" json:"-"`  // 密码返回时隐藏
	TokenVersion int    `gorm:"default:0" json:"-"` // Token 版本号，用于强制下线
}
