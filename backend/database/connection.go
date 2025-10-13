package database

import (
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
    "log"
)

var DB *gorm.DB

func Init() error {
    // ✅ CORRECTO: Usar el puerto interno del contenedor PostgreSQL
    dsn := "host=database user=local password=local dbname=spotify port=5432 sslmode=disable"
    
    var err error
    DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
    if err != nil {
        log.Printf("Error conectando a la base de datos: %v", err)
        return err
    }
    
    log.Println("✅ Conectado a la base de datos exitosamente")
    return nil
}