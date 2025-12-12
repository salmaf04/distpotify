package main

import (
	"bytes"
	"distributed-systems-project/models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
)

func (s *Server) syncDataFromLeader() {
	// Followers obtienen datos del líder
	s.mu.RLock()
	leaderID := s.leaderID
	myID := s.nodeID
	isLeader := s.isLeader
	s.mu.RUnlock()

	if isLeader || leaderID <= 0 || leaderID == myID {
		return
	}

	log.Printf("Nodo %d sincronizando datos del líder %d", myID, leaderID)

	// Construir URL al endpoint de snapshot del líder
	url := s.nodeURL(leaderID) + "/internal/songs/snapshot"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		log.Printf("Error obteniendo snapshot del líder %d: %v", leaderID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Líder %d devolvió status %d al pedir snapshot", leaderID, resp.StatusCode)
		return
	}

	// Estructura de respuesta
	var result struct {
		Songs  []models.Song `json:"songs"`
		Count  int           `json:"count"`
		NodeID int           `json:"node_id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decodificando snapshot de Songs: %v", err)
		return
	}

	log.Printf("Nodo %d recibió snapshot de Songs con %d canciones", myID, result.Count)

	// Insertar/actualizar en mi DB
	tx := s.db.Begin()
	if tx.Error != nil {
		log.Printf("Error iniciando transacción para sync: %v", tx.Error)
		return
	}

	// Opcional: limpiar tabla primero si quieres un mirror exacto
	if err := tx.Exec("DELETE FROM songs").Error; err != nil {
		log.Printf("Error limpiando tabla songs: %v", err)
		tx.Rollback()
		return
	}

	for _, song := range result.Songs {
		tx.Create(&song)
	}

	var synced_songs []models.Song
	if err := s.db.Find(&synced_songs).Error; err != nil {
		log.Printf("Error obteniendo canciones para sync: %v", err)
		return
	}

	log.Printf("Nodo %d sincronizó %d canciones desde líder %d", myID, len(synced_songs), leaderID)

	if err := tx.Commit().Error; err != nil {
		log.Printf("Error haciendo commit de sync desde líder: %v", err)
		return
	}

	log.Printf("Nodo %d sincronizó %d canciones desde líder %d", myID, len(result.Songs), leaderID)
}

func (s *Server) sendSongToNode(nodeID int, song models.Song) {
	log.Printf("Enviando canción '%s' a nodo %d", song.Title, nodeID)

	// Estructura de request que debe coincidir con lo que espera syncHandler
	type SyncRequest struct {
		Songs  []models.Song `json:"songs"`
		NodeID int           `json:"node_id"`
	}

	reqBody := SyncRequest{
		Songs:  []models.Song{song}, // solo la canción nueva
		NodeID: s.nodeID,            // quién envía
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Printf("Error serializando canción para nodo %d: %v", nodeID, err)
		return
	}

	basePort := 8080
	internalPort := basePort
	if nodeID > 1 {
		internalPort = basePort + (nodeID - 1)
	}
	url := fmt.Sprintf("http://backend%d:%d/internal/sync", nodeID, internalPort)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error enviando canción a nodo %d: %v", nodeID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Nodo %d devolvió status %d al sincronizar canción", nodeID, resp.StatusCode)
		return
	}

	log.Printf("Canción '%s' sincronizada correctamente con nodo %d", song.Title, nodeID)
}

// Handler para sync interno
func (s *Server) syncHandler(c *fiber.Ctx) error {
	type SyncRequest struct {
		Songs  []models.Song `json:"songs"`
		NodeID int           `json:"node_id"`
	}

	var req SyncRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Procesar sincronización
	for _, song := range req.Songs {
		song.ID = 0
		// Usar Upsert (crear o actualizar)
		result := s.db.Create(&song)

		if result.Error != nil {
			log.Printf("Error sincronizando canción %s: %v", song.Title, result.Error)
		}
	}

	log.Printf("Nodo %d sincronizó %d canciones desde nodo %d",
		s.nodeID, len(req.Songs), req.NodeID)

	return c.JSON(fiber.Map{
		"message": "Sync completed",
		"synced":  len(req.Songs),
		"node_id": s.nodeID,
	})
}

func (s *Server) initialSyncAfterJoin() {
	leaderID := s.discoverLeaderByScanning()
	if leaderID > 0 {
		s.mu.Lock()
		s.leaderID = leaderID
		s.mu.Unlock()

		log.Printf("Nodo %d usará líder %d para initial sync", s.nodeID, leaderID)
		s.syncDataFromLeader()
		return
	}

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			log.Printf("Nodo %d: timeout esperando líder para initial sync", s.nodeID)
			return
		case <-ticker.C:
			s.mu.RLock()
			leaderID = s.leaderID
			isLeader := s.isLeader
			s.mu.RUnlock()

			if leaderID > 0 && !isLeader {
				log.Printf("Nodo %d detectó líder %d, iniciando initial sync...", s.nodeID, leaderID)
				s.syncDataFromLeader()
				return
			}
		}
	}
}

func (s *Server) songsSnapshotHandler(c *fiber.Ctx) error {
	// Solo el líder debería servir este endpoint de verdad
	s.mu.RLock()
	isLeader := s.isLeader
	s.mu.RUnlock()
	if !isLeader {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":     "Solo el líder puede proveer snapshot de Songs",
			"node_id":   s.nodeID,
			"leader_id": s.leaderID,
			"is_leader": s.isLeader,
			"action":    "request_snapshot_on_leader",
		})
	}
	var songs []models.Song
	if err := s.db.Find(&songs).Error; err != nil {
		log.Printf("Error obteniendo snapshot de Songs: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error fetching songs from DB",
		})
	}
	return c.JSON(fiber.Map{
		"songs":   songs,
		"count":   len(songs),
		"node_id": s.nodeID,
	})
}
