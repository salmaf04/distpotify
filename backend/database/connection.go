package database

import (
	"log"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var DB *gorm.DB

func Init() error {
	// Usar variables de entorno o valores por defecto
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "database" // nombre del servicio en docker-compose
	}

	user := os.Getenv("DB_USER")
	if user == "" {
		user = "local"
	}

	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		password = "local"
	}

	dbname := os.Getenv("DB_NAME")
	if dbname == "" {
		dbname = "spotify"
	}

	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}

	dsn := "host=" + host + " user=" + user + " password=" + password +
		" dbname=" + dbname + " port=" + port + " sslmode=disable"

	log.Printf("Intentando conectar a: host=%s, dbname=%s", host, dbname)

	var err error
	DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Printf("Error conectando a la base de datos: %v", err)
		return err
	}

	log.Println("✅ Conectado a la base de datos exitosamente")
	return nil
}
