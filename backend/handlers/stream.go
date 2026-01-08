package handlers

import (
	"distributed-systems-project/models"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

type StreamHandler struct {
	DB       *gorm.DB
	SongsDir string
}

type byteRange struct {
	start int64
	end   int64
}

func (h *StreamHandler) StreamSong(c *fiber.Ctx) error {
	songID, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "ID de canción inválido"})
	}

	// Obtener metadatos de la canción desde PostgreSQL
	var song models.Song
	if err := h.DB.First(&song, songID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return c.Status(404).JSON(fiber.Map{"error": "Canción no encontrada"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "Error al buscar canción"})
	}

	// Construir ruta completa del archivo
	filePath := filepath.Join(h.SongsDir, song.File)

	// Verificar que el archivo existe
	file, err := os.Open(filePath)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "Archivo de audio no encontrado"})
	}
	defer file.Close()

	// Obtener información del archivo
	fileInfo, err := file.Stat()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Error al obtener información del archivo"})
	}

	// Obtener rango solicitado
	rangeHeader := c.Get("Range")

	if rangeHeader == "" {
		// Enviar todo el archivo si no se solicita rango específico
		c.Set("Content-Type", getContentType(song.FileFormat))
		c.Set("Content-Length", strconv.FormatInt(fileInfo.Size(), 10))
		c.Set("Accept-Ranges", "bytes")
		return c.SendStream(file)
	}

	// Parsear y manejar rangos
	bytesRange, err := parseRange(rangeHeader, fileInfo.Size())

	start := bytesRange[0].start
	end := bytesRange[0].end

	if err != nil {
		return c.Status(416).JSON(fiber.Map{"error": "Rango no satisfacible"})
	}

	// Configurar headers para respuesta parcial (206)
	contentLength := end - start + 1
	c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileInfo.Size()))
	c.Set("Accept-Ranges", "bytes")
	c.Set("Content-Length", strconv.FormatInt(contentLength, 10))
	c.Set("Content-Type", getContentType(song.FileFormat))
	c.Status(http.StatusPartialContent)

	// Enviar el chunk solicitado
	return sendChunk(c, file, start, contentLength)
}

func parseRange(rangeHeader string, fileSize int64) ([]byteRange, error) {
	// Ejemplo: "bytes=0-1023"
	rangeHeader = strings.Replace(rangeHeader, "bytes=", "", 1)
	ranges := strings.Split(rangeHeader, "-")

	if len(ranges) != 2 {
		return nil, fmt.Errorf("formato de rango inválido")
	}

	start, err := strconv.ParseInt(ranges[0], 10, 64)
	if err != nil {
		return nil, err
	}

	end, err := strconv.ParseInt(ranges[1], 10, 64)
	if err != nil {
		// Si no se especifica el final, ir hasta el final del archivo
		end = fileSize - 1
	}

	// Validar rangos
	if start < 0 || end >= fileSize || start > end {
		return nil, fmt.Errorf("rango fuera de límites")
	}

	return []byteRange{{start, end}}, nil
}

func sendChunk(c *fiber.Ctx, file *os.File, start, length int64) error {
	_, err := file.Seek(start, io.SeekStart)
	if err != nil {
		return err
	}

	buffer := make([]byte, length)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return err
	}

	return c.Send(buffer[:n])
}

// getContentType retorna el Content-Type según el formato del archivo
func getContentType(format string) string {
	switch strings.ToLower(format) {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "ogg":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "m4a":
		return "audio/mp4"
	case "aac":
		return "audio/aac"
	default:
		return "audio/mpeg"
	}
}
