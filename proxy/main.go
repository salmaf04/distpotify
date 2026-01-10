package main

import (
	"os"
	proxy "proxy/core" // módulo = proxy, subpaquete = core

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func main() {
	app := fiber.New(fiber.Config{BodyLimit: 100 * 1024 * 1024}) // 100 MB
	// Configurar CORS: permitir origen configurable via env ALLOWED_ORIGINS
	allowed := os.Getenv("ALLOWED_ORIGINS")
	if allowed == "" {
		// por defecto permitir localhost de desarrollo
		allowed = "http://localhost:8080"
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins:     allowed,
		AllowCredentials: true,
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Requested-With",
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS,PATCH",
	}))
	rp := proxy.NewReverseProxy("", 8080)
	// Exporta el handler en core como CreateProxyHandler, no createProxyHandler
	// Creas el proxy UNA SOLA VEZ
	proxyHandler := rp.CreateProxyHandler()

	// Lo usas en varias rutas
	app.All("/api/*", proxyHandler)
	app.All("/*", proxyHandler) // catch-all
	
	port := os.Getenv("API_PORT")
	if port == "" {
	    port = "8081" // valor por defecto
	}

	app.Listen(":" + port)
}
