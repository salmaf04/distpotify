package utils

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func SaveFile(c *fiber.Ctx, file *multipart.FileHeader, artist string, title string) (string, error) {
	// Usar ruta relativa dentro del contenedor
	dir := filepath.Join("storage", "songs")

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("error obteniendo ruta absoluta: %v", err)
	}

	log.Printf("Guardando archivo en: %s", absDir)

	if err := os.MkdirAll(absDir, 0755); err != nil {
		return "", fmt.Errorf("error creando directorio destino: %v", err)
	}

	safeArtist := sanitizeFileName(artist)
	safeTitle := sanitizeFileName(title)

	filename := fmt.Sprintf("%s-%s.mp3", safeArtist, safeTitle)
	destPath := filepath.Join(absDir, filename)

	dst, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("error al guardar archivo: %v", err)
	}
	defer dst.Close()

	src, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("error al abrir archivo: %v", err)
	}
	defer src.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("error al copiar archivo: %v", err)
	}

	// Guardar solo el nombre del archivo
	// El SongsDir del handler ya es "storage/songs"
	return filename, nil
}

func sanitizeFileName(name string) string {
	reg := regexp.MustCompile(`[<>:"/\\|?*]`)
	safe := reg.ReplaceAllString(name, "_")
	if len(safe) > 100 {
		safe = safe[:100]
	}
	return strings.TrimSpace(safe)
}
