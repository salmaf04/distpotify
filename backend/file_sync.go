package main

import (
	"bytes"
	"distributed-systems-project/models"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

// FileReplicationStatus representa el estado de replicación de un archivo
type FileReplicationStatus struct {
	Filename  string
	NodeID    int
	Status    string // "pending", "success", "failed"
	Error     string
	Timestamp time.Time
}

// ReplicateFilesToFollowers envía el archivo MP3 a todos los followers
// Se ejecuta después de guardar el archivo en el nodo líder
func (s *Server) ReplicateFilesToFollowers(filename string, filePath string) {
	s.mu.RLock()
	isLeader := s.isLeader
	s.mu.RUnlock()

	if !isLeader {
		log.Printf("Nodo %d: No soy líder, no replico archivos", s.nodeID)
		return
	}

	log.Printf("Nodo %d: Iniciando replicación de archivo %s a followers", s.nodeID, filename)

	// Obtener lista de followers (todos menos el líder)
	followers := s.getAllNodeIDs()
	var filteredFollowers []int
	for _, nodeID := range followers {
		if nodeID != s.nodeID {
			filteredFollowers = append(filteredFollowers, nodeID)
		}
	}

	// Replicar a todos los followers en paralelo
	var wg sync.WaitGroup
	results := make(chan FileReplicationStatus, len(filteredFollowers))

	for _, followerID := range filteredFollowers {
		wg.Add(1)
		go func(fID int) {
			defer wg.Done()
			status := s.replicateFileToNode(filename, filePath, fID)
			results <- status
		}(followerID)
	}

	// Esperar a que terminen todas las replicaciones
	go func() {
		wg.Wait()
		close(results)
	}()

	// Registrar resultados
	var successCount, failCount int
	for status := range results {
		if status.Status == "success" {
			successCount++
			log.Printf("✓ Archivo %s replicado exitosamente a nodo %d", filename, status.NodeID)
		} else {
			failCount++
			log.Printf("✗ Error replicando %s a nodo %d: %s", filename, status.NodeID, status.Error)
		}
	}

	log.Printf("Replicación completada: %d exitosos, %d fallos", successCount, failCount)
}

// replicateFileToNode envía el archivo a un nodo follower específico
func (s *Server) replicateFileToNode(filename string, filePath string, nodeID int) FileReplicationStatus {
	status := FileReplicationStatus{
		Filename:  filename,
		NodeID:    nodeID,
		Timestamp: time.Now(),
	}

	// Abrir archivo
	file, err := os.Open(filePath)
	if err != nil {
		status.Status = "failed"
		status.Error = fmt.Sprintf("Error abriendo archivo: %v", err)
		return status
	}
	defer file.Close()

	// Crear multipart request
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Agregar archivo al multipart
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		status.Status = "failed"
		status.Error = fmt.Sprintf("Error creando form file: %v", err)
		return status
	}

	if _, err := io.Copy(part, file); err != nil {
		status.Status = "failed"
		status.Error = fmt.Sprintf("Error copiando archivo: %v", err)
		return status
	}

	// Cerrar el writer para finalizar el multipart
	if err := writer.Close(); err != nil {
		status.Status = "failed"
		status.Error = fmt.Sprintf("Error cerrando writer: %v", err)
		return status
	}

	// Hacer request POST al follower
	nodeURL := s.nodeURL(nodeID)
	url := fmt.Sprintf("%s/internal/receive-file", nodeURL)

	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		status.Status = "failed"
		status.Error = fmt.Sprintf("Error creando request: %v", err)
		return status
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Filename", filename)
	req.Header.Set("X-Source-Node", fmt.Sprintf("%d", s.nodeID))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		status.Status = "failed"
		status.Error = fmt.Sprintf("Error enviando request: %v", err)
		return status
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		status.Status = "failed"
		status.Error = fmt.Sprintf("Status %d: %s", resp.StatusCode, string(body))
		return status
	}

	status.Status = "success"
	return status
}

// Handler para recibir archivos replicados desde el líder
func (s *Server) receiveFileHandler(c *fiber.Ctx) error {
	// Obtener el archivo del multipart form
	file, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "No se encontró archivo en la solicitud",
		})
	}

	filename := c.Get("X-Filename")
	if filename == "" {
		filename = file.Filename
	}

	sourceNode := c.Get("X-Source-Node")
	log.Printf("Nodo %d: Recibiendo archivo %s desde nodo %s", s.nodeID, filename, sourceNode)

	// Crear directorio si no existe
	songsDir := "storage/songs"
	if err := os.MkdirAll(songsDir, 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("Error creando directorio: %v", err),
		})
	}

	// Guardar archivo
	destPath := filepath.Join(songsDir, filename)
	if err := c.SaveFile(file, destPath); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": fmt.Sprintf("Error guardando archivo: %v", err),
		})
	}

	log.Printf("✓ Nodo %d guardó archivo %s desde nodo %s", s.nodeID, filename, sourceNode)

	return c.JSON(fiber.Map{
		"message":   "Archivo recibido exitosamente",
		"filename":  filename,
		"node_id":   s.nodeID,
		"file_size": file.Size,
	})
}

// DownloadFileFromLeader descarga un archivo específico del líder
// Se usa cuando el follower necesita un archivo que no tiene
func (s *Server) DownloadFileFromLeader(filename string) error {
	s.mu.RLock()
	leaderID := s.leaderID
	s.mu.RUnlock()

	if leaderID <= 0 {
		return fmt.Errorf("no se encontró líder para descargar archivo")
	}

	nodeURL := s.nodeURL(leaderID)
	url := fmt.Sprintf("%s/internal/download-file?filename=%s", nodeURL, filename)

	log.Printf("Nodo %d: Descargando archivo %s desde líder %d", s.nodeID, filename, leaderID)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("error descargando archivo: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("líder devolvió status %d", resp.StatusCode)
	}

	// Crear directorio si no existe
	songsDir := "storage/songs"
	if err := os.MkdirAll(songsDir, 0755); err != nil {
		return fmt.Errorf("error creando directorio: %v", err)
	}

	// Guardar archivo
	destPath := filepath.Join(songsDir, filename)
	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("error creando archivo destino: %v", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, resp.Body); err != nil {
		return fmt.Errorf("error copiando archivo: %v", err)
	}

	log.Printf("✓ Nodo %d descargó exitosamente %s desde líder %d", s.nodeID, filename, leaderID)
	return nil
}

// Handler para servir archivos a otros nodos (solo líder)
func (s *Server) downloadFileHandler(c *fiber.Ctx) error {
	s.mu.RLock()
	isLeader := s.isLeader
	s.mu.RUnlock()

	if !isLeader {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Solo el líder puede servir archivos",
		})
	}

	filename := c.Query("filename")
	if filename == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "El parámetro 'filename' es requerido",
		})
	}

	// Validar que el filename no contiene path traversal
	if filepath.IsAbs(filename) || filepath.Base(filename) != filename {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Nombre de archivo inválido",
		})
	}

	filePath := filepath.Join("storage/songs", filename)

	// Verificar que el archivo existe
	if _, err := os.Stat(filePath); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Archivo no encontrado",
		})
	}

	log.Printf("Nodo %d (líder): Sirviendo archivo %s", s.nodeID, filename)

	return c.SendFile(filePath)
}

// SyncMissingFilesFromLeader sincroniza archivos faltantes desde el líder
// Se ejecuta periódicamente para asegurar consistencia de archivos
func (s *Server) SyncMissingFilesFromLeader() {
	s.mu.RLock()
	leaderID := s.leaderID
	isLeader := s.isLeader
	s.mu.RUnlock()

	if isLeader || leaderID <= 0 {
		return
	}

	// Obtener lista de canciones de la BD
	var songs []models.Song
	if err := s.db.Find(&songs).Error; err != nil {
		log.Printf("Error obteniendo lista de canciones: %v", err)
		return
	}

	// Verificar que cada canción tiene su archivo
	songsDir := "storage/songs"
	for _, song := range songs {
		filePath := filepath.Join(songsDir, song.File)

		// Si el archivo no existe, descargarlo del líder
		if _, err := os.Stat(filePath); err != nil {
			log.Printf("Archivo faltante: %s, descargando desde líder %d", song.File, leaderID)
			if err := s.DownloadFileFromLeader(song.File); err != nil {
				log.Printf("Error sincronizando archivo %s: %v", song.File, err)
			}
		}
	}
}
