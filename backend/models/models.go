package models

import (
    "time"
    "gorm.io/gorm"
)

type Song struct {
    ID        uint           `gorm:"primaryKey" json:"id"`
    Title     string         `gorm:"size:255;not null" json:"title"`
    Artist    string         `gorm:"size:255;not null" json:"artist"`
    Album     string         `gorm:"size:255" json:"album"`
    Genre     string         `gorm:"size:100" json:"genre"`
    Duration  string         `gorm:"size:10" json:"duration"`
    File      string         `gorm:"not null" json:"file"`
    Cover     string         `json:"cover"`
}

type Playlist struct {
    ID          uint           `gorm:"primaryKey" json:"id"`
    Name        string         `gorm:"size:255;not null" json:"name"`
    Description string         `gorm:"type:text" json:"description"`
    UserID      uint           `json:"user_id"`
    Songs       []Song         `gorm:"many2many:playlist_songs;" json:"songs"`
    CreatedAt   time.Time      `json:"created_at"`
    UpdatedAt   time.Time      `json:"updated_at"`
    DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}