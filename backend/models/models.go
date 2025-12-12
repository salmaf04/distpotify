package models

import (
	"time"

	"gorm.io/gorm"
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
