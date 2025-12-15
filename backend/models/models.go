package models

import (
	"time"

	"gorm.io/gorm"
)

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type Song struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Title     string         `gorm:"not null;index" json:"title"`
	Artist    string         `gorm:"not null;index" json:"artist"`
	Album     string         `json:"album"`
	Genre     string         `json:"genre"`
	Duration  string         `json:"duration"`
	File      string         `gorm:"column:file_path" json:"file_path"`
	Cover     string         `gorm:"column:cover_path" json:"cover_path,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type User struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Username  string         `gorm:"uniqueIndex;not null" json:"username"`
	Password  string         `gorm:"not null" json:"-"` // No devolver password en JSON
	Role      Role           `gorm:"default:'user'" json:"role"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

type OperationLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Index     int64     `gorm:"uniqueIndex;not null" json:"index"`
	Type      string    `gorm:"not null" json:"type"`
	Data      []byte    `gorm:"type:jsonb" json:"data"`
	UserData  []byte    `gorm:"type:jsonb" json:"user_data"`
	Timestamp int64     `json:"timestamp"`
	CreatedAt time.Time `json:"created_at"`
}
