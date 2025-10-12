package database

import (
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

var DB *gorm.DB

func Init() error {
    dsn := "host=localhost user=postgres password=postgres dbname=spotify port=5434 sslmode=disable"
    
    var err error
    DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        return err
    }
    
    return nil
}