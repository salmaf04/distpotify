package utils

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func SaveFile(c *fiber.Ctx, file *multipart.FileHeader, artist string, title string) (string, error) {
	// Subir un nivel desde utils y entrar a storage/songs
	dir := filepath.Join("..", "storage", "songs")

	// Obtener la ruta absoluta para mejor control
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("error obteniendo ruta absoluta: %v", err)
	}

	// Asegurar directorio de destino
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("error creando directorio destino: %v", err)
	}

	// Sanitizar nombres de archivo (recomendado)
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

	return destPath, nil
}

// Función auxiliar para sanitizar nombres de archivo
func sanitizeFileName(name string) string {
	// Remover o reemplazar caracteres problemáticos
	reg := regexp.MustCompile(`[<>:"/\\|?*]`)
	safe := reg.ReplaceAllString(name, "_")
	// Limitar longitud si es necesario
	if len(safe) > 100 {
		safe = safe[:100]
	}
	return strings.TrimSpace(safe)
}
