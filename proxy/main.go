package main

import (
	"log"
	proxy "proxy/core" // módulo = proxy, subpaquete = core

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	rp := proxy.NewReverseProxy("", 8080)
	// Exporta el handler en core como CreateProxyHandler, no createProxyHandler
	// Creas el proxy UNA SOLA VEZ
	proxyHandler := rp.CreateProxyHandler()

	// Lo usas en varias rutas
	app.All("/api/*", proxyHandler)
	app.All("/*", proxyHandler) // catch-all
	log.Fatal(app.Listen(":8081"))
}
