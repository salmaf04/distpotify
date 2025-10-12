package main

import (
    "log"
    "fmt"
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/cors"
    "github.com/gofiber/fiber/v2/middleware/logger"
    
    "distributed-systems-project/database"
    "distributed-systems-project/handlers"
    "distributed-systems-project/models"
)

func main() {
    // Conectar a PostgreSQL
    if err := database.Init(); err != nil {
        log.Fatal("Error conectando a la base de datos:", err)
    }
    
    // Migraciones
    database.DB.AutoMigrate(&models.Song{}, &models.Playlist{})
    
    app := fiber.New()
    
    // Middlewares
    app.Use(logger.New())
    app.Use(cors.New())
    
    // Handlers
    SongHandler := &handlers.SongHandler{DB: database.DB}
    
    // Rutas
    app.Get("/api/songs", SongHandler.GetAllSongs)
    app.Get("/api/songs/:id", SongHandler.GetSongByID)
    app.Get("/api/songs/search", SongHandler.GetSongsSearch)
    app.Post("/api/songs", SongHandler.CreateSong)
    
    app.Static("/music", "./storage/canciones")
    app.Static("/images", "./storage/portadas")
    
    var PORT string = "3003"

    log.Printf("🎵 Spotify API ejecutándose en http://localhost:%s", PORT)
    log.Fatal(app.Listen(fmt.Sprintf(":%s", PORT)))
}