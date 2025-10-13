package utils 

import (
	"fmt"
	"os"
	"io"
	"mime/multipart"
	"path/filepath"
	"github.com/gofiber/fiber/v2"
)

func SaveFile(c *fiber.Ctx, file *multipart.FileHeader, artist string, title string) (string, error) {
    // asegurar directorio de destino
    dir := filepath.Join(".", "storage", "songs")
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return "", fmt.Errorf("error creando directorio destino: %v", err)
    }

    filename := fmt.Sprintf("%s-%s.mp3", artist, title)
    destPath := filepath.Join(dir, filename)

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