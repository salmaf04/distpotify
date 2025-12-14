package main

import (
	"bytes"
	"distributed-systems-project/models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

func (s *Server) syncDataFromLeader() {
	s.mu.RLock()
	leaderID := s.leaderID
	lastIndex := s.lastAppliedIndex
	s.mu.RUnlock()

	if leaderID <= 0 {
		return
	}

	// 1. INTENTAR DELTA SYNC
	url := fmt.Sprintf("%s/internal/sync/delta?since=%d", s.nodeURL(leaderID), lastIndex)
	resp, err := http.Get(url)

	if err == nil && resp.StatusCode == http.StatusOK {
		var result struct {
			Operations []Operation `json:"operations"`
		}
		if json.NewDecoder(resp.Body).Decode(&result) == nil {
			log.Printf("Recibidas %d operaciones delta", len(result.Operations))
			s.applyOperations(result.Operations)
			return // Éxito
		}
	}

	// 2. FALLBACK A SNAPSHOT (Si falla delta o devuelve 409 Conflict)
	log.Printf("Delta sync falló o insuficiente. Solicitando Snapshot completo...")

	// Construir URL al endpoint de snapshot del líder
	url = s.nodeURL(leaderID) + "/internal/songs/snapshot"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err = client.Get(url)
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

	log.Printf("Nodo %d recibió snapshot de Songs con %d canciones", s.nodeID, result.Count)

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

	log.Printf("Nodo %d sincronizó %d canciones desde líder %d", s.nodeID, len(synced_songs), leaderID)

	if err := tx.Commit().Error; err != nil {
		log.Printf("Error haciendo commit de sync desde líder: %v", err)
		return
	}

	log.Printf("Nodo %d sincronizó %d canciones desde líder %d", s.nodeID, len(result.Songs), leaderID)
}

func (s *Server) applyOperations(ops []Operation) {
	for _, op := range ops {
		switch op.Type {
		case OpCreate:
			s.db.Create(&op.Data) // Usar Clauses(OnConflict) es mejor
			// case OpUpdate: ...
			// case OpDelete: ...
		}

		// Actualizar índice local
		s.mu.Lock()
		if op.Index > s.lastAppliedIndex {
			s.lastAppliedIndex = op.Index
		}
		s.mu.Unlock()
	}
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

	url := fmt.Sprintf("http://backend%d:3003/internal/sync", nodeID)
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

// replicateToFollowers envía la canción a todos los followers y espera un mínimo de ACKs
func (s *Server) replicateToFollowers(song models.Song, minAcks int) bool {
	followers := s.getAllNodeIDs()

	// Filtrar mi propio ID
	var targetNodes []int
	for _, id := range followers {
		if id != s.nodeID {
			targetNodes = append(targetNodes, id)
		}
	}

	totalFollowers := len(targetNodes)
	if totalFollowers == 0 {
		// Si soy el único nodo, se considera éxito inmediato
		return true
	}

	// Ajustar minAcks si hay menos nodos disponibles que lo requerido
	if totalFollowers < minAcks {
		minAcks = totalFollowers
		log.Printf("Ajustando minAcks a %d porque solo hay %d followers", minAcks, totalFollowers)
	}

	var wg sync.WaitGroup
	ackChan := make(chan bool, totalFollowers)

	for _, nodeID := range targetNodes {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			type SyncRequest struct {
				Songs  []models.Song `json:"songs"`
				NodeID int           `json:"node_id"`
			}
			reqBody := SyncRequest{
				Songs:  []models.Song{song},
				NodeID: s.nodeID,
			}
			jsonData, _ := json.Marshal(reqBody)
			url := fmt.Sprintf("http://backend%d:3003/internal/sync", id)
			client := &http.Client{Timeout: 2 * time.Second} // Timeout corto para no bloquear

			resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
			if err == nil && resp.StatusCode == http.StatusOK {
				ackChan <- true
			} else {
				ackChan <- false
			}
			if resp != nil {
				resp.Body.Close()
			}
		}(nodeID)
	}

	// Cerrar canal cuando todos terminen
	go func() {
		wg.Wait()
		close(ackChan)
	}()

	acksReceived := 0
	for success := range ackChan {
		if success {
			acksReceived++
		}
	}

	log.Printf("Replicación completada: %d/%d ACKs recibidos (min requerido: %d)", acksReceived, totalFollowers, minAcks)
	return acksReceived >= minAcks
}
