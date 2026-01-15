package main

import (
	"bytes"
	"distributed-systems-project/models"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gofiber/fiber/v2"
)

type ReplicationUser struct {
	ID        uint        `json:"id"`
	Username  string      `json:"username"`
	Password  string      `json:"password"`
	Role      models.Role `json:"role"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type ReplicationSession struct {
	ID           string    `json:"id"`
	UserID       uint      `json:"user_id"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	LastActivity time.Time `json:"last_activity"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type SnapshotData struct {
	Songs            []models.Song         `json:"songs"`
	Sessions         []ReplicationSession  `json:"sessions"`
	Users            []ReplicationUser     `json:"users"`
	OperationLogs    []models.OperationLog `json:"operation_logs"`
	LastAppliedIndex int64                 `json:"last_applied_index"`
	NodeID           int                   `json:"node_id"`
}

func (s *Server) PropagateSnapshotToFollowers() {
	log.Println("⚡ LÍDER: Propagando snapshot completo a todos los followers...")

	// 1. Recolectar estado actual
	var songs []models.Song
	s.db.Find(&songs)

	var users []models.User
	s.db.Find(&users)

	var sessions []models.Session
	s.db.Find(&sessions)

	var logs []models.OperationLog
	s.db.Find(&logs)

	// Convertir a estructuras de replicación
	var repUsers []ReplicationUser
	for _, u := range users {
		repUsers = append(repUsers, ReplicationUser{
			ID: u.ID, Username: u.Username, Password: u.Password,
			Role: u.Role, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		})
	}

	var repSessions []ReplicationSession
	for _, sess := range sessions {
		repSessions = append(repSessions, ReplicationSession{
			ID: sess.ID, UserID: sess.UserID, IPAddress: sess.IPAddress,
			UserAgent: sess.UserAgent, LastActivity: sess.LastActivity, ExpiresAt: sess.ExpiresAt,
		})
	}

	lastIndex, _ := s.opLog.GetLastOperationInfo()

	snapshot := SnapshotData{
		Songs:            songs,
		Users:            repUsers,
		Sessions:         repSessions,
		OperationLogs:    logs,
		LastAppliedIndex: lastIndex,
		NodeID:           s.nodeID,
	}

	// 2. Enviar a todos los nodos en paralelo
	followers := s.getAllNodeIDs()
	for _, id := range followers {
		if id == s.nodeID {
			continue
		}

		go func(targetID int) {
			url := fmt.Sprintf("%s/internal/sync/receive-snapshot", s.nodeURL(targetID))
			jsonData, _ := json.Marshal(snapshot)

			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))

			if err != nil {
				log.Printf("❌ Error enviando snapshot a nodo %d: %v", targetID, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				log.Printf("✅ Snapshot entregado correctamente a nodo %d", targetID)
			}
		}(id)
	}
}

// Handler para recibir y APLICAR el snapshot forzado
func (s *Server) receiveSnapshotHandler(c *fiber.Ctx) error {
	var data SnapshotData
	if err := c.BodyParser(&data); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	log.Printf("📥 Recibiendo SNAPSHOT FORZADO del líder %d. Sobreescribiendo estado local...", data.NodeID)

	// Actualizar líder
	s.mu.Lock()
	s.leaderID = data.NodeID
	s.isLeader = false
	s.mu.Unlock()

	tx := s.db.Begin()
	if tx.Error != nil {
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	// 1. Limpiar todo (TRUNCATE)
	tx.Exec("TRUNCATE TABLE songs RESTART IDENTITY CASCADE")
	tx.Exec("TRUNCATE TABLE sessions RESTART IDENTITY CASCADE")
	tx.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE")
	tx.Exec("TRUNCATE TABLE operation_logs RESTART IDENTITY CASCADE")

	// 2. Insertar datos recibidos
	for _, song := range data.Songs {
		tx.Create(&song)
	}
	for _, u := range data.Users {
		tx.Create(&models.User{
			ID: u.ID, Username: u.Username, Password: u.Password,
			Role: u.Role, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
		})
	}
	for _, sess := range data.Sessions {
		tx.Create(&models.Session{
			ID: sess.ID, UserID: sess.UserID, IPAddress: sess.IPAddress,
			UserAgent: sess.UserAgent, LastActivity: sess.LastActivity, ExpiresAt: sess.ExpiresAt,
		})
	}
	for _, op := range data.OperationLogs {
		tx.Create(&op)
	}

	if err := tx.Commit().Error; err != nil {
		log.Printf("Error aplicando snapshot: %v", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	s.fixSequences()

	// 3. IMPORTANTE: Disparar sincronización de archivos MP3 faltantes
	// Esto solo descargará los archivos que NO existan en disco
	go s.SyncMissingFilesFromLeader()

	return c.SendStatus(fiber.StatusOK)
}

func (s *Server) fixSequences() {
	// Arreglar secuencia de Songs
	s.db.Exec("SELECT setval('songs_id_seq', (SELECT MAX(id) FROM songs));")

	// Arreglar secuencia de Users
	s.db.Exec("SELECT setval('users_id_seq', (SELECT MAX(id) FROM users));")

	// Arreglar secuencia de OperationLogs (IMPORTANTE)
	s.db.Exec("SELECT setval('operation_logs_id_seq', (SELECT MAX(id) FROM operation_logs));")

	// Arreglar secuencia de Sessions (si aplica)
	s.db.Exec("SELECT setval('sessions_id_seq', (SELECT MAX(id) FROM sessions));")

	log.Println("Secuencias de DB actualizadas correctamente tras sincronización.")
}

func (s *Server) syncDataFromLeader() {
	s.mu.RLock()
	leaderID := s.leaderID
	lastIndex, _ := s.opLog.GetLastOperationInfo()
	s.mu.RUnlock()

	if leaderID <= 0 {
		return
	}

	// 1. INTENTAR DELTA SYNC SOLO SI NO ES PRIMER SYNC (lastIndex > 0)
	// En el primer sync (lastIndex == 0), hacer snapshot completo para restaurar usuarios, sesiones, y operation_logs
	if lastIndex > 0 {
		url := fmt.Sprintf("%s/internal/sync/delta?since=%d", s.nodeURL(leaderID), lastIndex)
		resp, err := http.Get(url)

		if err == nil && resp.StatusCode == http.StatusOK {
			var result struct {
				Operations []Operation `json:"operations"`
			}
			if json.NewDecoder(resp.Body).Decode(&result) == nil {
				log.Printf("Recibidas %d operaciones delta", len(result.Operations))
				s.applyOperations(result.Operations)
				// === CORRECCIÓN AQUÍ ===
				// Iniciar descarga de archivos faltantes en segundo plano tras el delta
				go s.SyncMissingFilesFromLeader()
				return // Éxito
			}
		}
	}

	// 2. FALLBACK A SNAPSHOT (Si falla delta, es primer sync, o devuelve 409 Conflict)
	log.Printf("Delta sync falló o insuficiente. Solicitando Snapshot completo...")

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

	// Estructura de respuesta expandida
	var result struct {
		Songs            []models.Song         `json:"songs"`
		Sessions         []ReplicationSession  `json:"sessions"`
		Users            []ReplicationUser     `json:"users"`
		OperationLogs    []models.OperationLog `json:"operation_logs"`
		LastAppliedIndex int64                 `json:"last_applied_index"`
		Count            int                   `json:"count"`
		NodeID           int                   `json:"node_id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("Error decodificando snapshot: %v", err)
		return
	}

	log.Printf("Nodo %d recibió snapshot con %d canciones, %d sesiones, %d usuarios y %d operation logs",
		s.nodeID, result.Count, len(result.Sessions), len(result.Users), len(result.OperationLogs))

	// Insertar/actualizar en mi DB con transacción
	tx := s.db.Begin()
	if tx.Error != nil {
		log.Printf("Error iniciando transacción para sync: %v", tx.Error)
		return
	}

	// Limpiar tablas primero para tener un mirror exacto
	// Usar TRUNCATE para reiniciar las secuencias de auto-incremento
	if err := tx.Exec("TRUNCATE TABLE songs RESTART IDENTITY CASCADE").Error; err != nil {
		log.Printf("Error limpiando tabla songs: %v", err)
		tx.Rollback()
		return
	}

	if err := tx.Exec("TRUNCATE TABLE sessions RESTART IDENTITY CASCADE").Error; err != nil {
		log.Printf("Error limpiando tabla sessions: %v", err)
		tx.Rollback()
		return
	}

	if err := tx.Exec("TRUNCATE TABLE users RESTART IDENTITY CASCADE").Error; err != nil {
		log.Printf("Error limpiando tabla users: %v", err)
		tx.Rollback()
		return
	}

	// Limpiar operation_logs con TRUNCATE para resetear la secuencia
	if err := tx.Exec("TRUNCATE TABLE operation_logs RESTART IDENTITY CASCADE").Error; err != nil {
		log.Printf("Error limpiando tabla operation_logs: %v", err)
		tx.Rollback()
		return
	}

	// Insertar canciones
	for _, song := range result.Songs {
		if err := tx.Create(&song).Error; err != nil {
			log.Printf("Error insertando canción %s: %v", song.Title, err)
			tx.Rollback()
			return
		}
	}

	// Insertar usuarios
	for _, userData := range result.Users {
		user := models.User{
			ID:        userData.ID,
			Username:  userData.Username,
			Password:  userData.Password,
			Role:      userData.Role,
			CreatedAt: userData.CreatedAt,
			UpdatedAt: userData.UpdatedAt,
		}
		if err := tx.Create(&user).Error; err != nil {
			log.Printf("Error insertando usuario %s: %v", user.Username, err)
			tx.Rollback()
			return
		}
	}

	// Insertar sesiones
	for _, sessData := range result.Sessions {
		session := models.Session{
			ID:           sessData.ID,
			UserID:       sessData.UserID,
			IPAddress:    sessData.IPAddress,
			UserAgent:    sessData.UserAgent,
			LastActivity: sessData.LastActivity,
			ExpiresAt:    sessData.ExpiresAt,
		}
		if err := tx.Create(&session).Error; err != nil {
			log.Printf("Error insertando sesión: %v", err)
			tx.Rollback()
			return
		}
	}

	// Insertar operation logs
	for _, opLog := range result.OperationLogs {
		if err := tx.Create(&opLog).Error; err != nil {
			log.Printf("Error insertando operation log: %v", err)
			tx.Rollback()
			return
		}
	}

	if err := tx.Exec("SELECT setval('songs_id_seq', COALESCE((SELECT MAX(id) FROM songs), 1))").Error; err != nil {
		log.Printf("Error actualizando seq songs: %v", err)
	}
	if err := tx.Exec("SELECT setval('users_id_seq', COALESCE((SELECT MAX(id) FROM users), 1))").Error; err != nil {
		log.Printf("Error actualizando seq users: %v", err)
	}
	// NUEVO: Arreglar secuencia de operation_logs
	if err := tx.Exec("SELECT setval('operation_logs_id_seq', COALESCE((SELECT MAX(id) FROM operation_logs), 1))").Error; err != nil {
		log.Printf("Error actualizando seq op_logs: %v", err)
	}

	// Commit de la transacción
	if err := tx.Commit().Error; err != nil {
		log.Printf("Error haciendo commit de sync desde líder: %v", err)
		return
	}

	log.Printf("Nodo %d sincronizó correctamente: %d canciones, %d sesiones, %d usuarios, %d operation logs. LastAppliedIndex=%d",
		s.nodeID, len(result.Songs), len(result.Sessions), len(result.Users), len(result.OperationLogs), result.LastAppliedIndex)

	// Marcar como sincronizado
	s.syncMutex.Lock()
	s.isSyncedWithLeader = true
	s.syncMutex.Unlock()

	go s.SyncMissingFilesFromLeader()
}

// Handler para sync interno
func (s *Server) syncHandler(c *fiber.Ctx) error {
	type SyncRequest struct {
		Songs    []models.Song        `json:"songs"`
		Sessions []ReplicationSession `json:"sessions"`
		Users    []ReplicationUser    `json:"users"` // Nuevo campo
		NodeID   int                  `json:"node_id"`
	}

	var req SyncRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	for _, song := range req.Songs {
		s.opLog.Append(OpCreate, song)

		if err := s.db.Create(&song).Error; err != nil {
			log.Printf("Error sincronizando usuario %s: %v", song.Title, err)
		}
	}

	log.Printf("Nodo %d sincronizó %d canciones desde nodo %d",
		s.nodeID, len(req.Songs), req.NodeID)

	for _, repUser := range req.Users {
		user := models.User{
			Username:  repUser.Username,
			Password:  repUser.Password,
			Role:      repUser.Role,
			CreatedAt: repUser.CreatedAt,
			UpdatedAt: repUser.UpdatedAt,
		}

		s.opLog.AppendUser(OpCreateUser, user)

		if err := s.db.Create(&user).Error; err != nil {
			log.Printf("Error sincronizando usuario %s: %v", user.Username, err)
		}
	}

	log.Printf("Nodo %d sincronizó %d usuarios desde nodo %d",
		s.nodeID, len(req.Users), req.NodeID)

	// Procesar sincronización de sesiones
	for _, sessData := range req.Sessions {
		session := models.Session{
			ID:           sessData.ID,
			UserID:       sessData.UserID,
			IPAddress:    sessData.IPAddress,
			UserAgent:    sessData.UserAgent,
			LastActivity: sessData.LastActivity,
			ExpiresAt:    sessData.ExpiresAt,
		}

		if err := s.db.Create(&session).Error; err != nil {
			log.Printf("Error sincronizando sesión %s: %v", session.ID, err)
		}

		s.opLog.AppendSession(OpCreateSession, session)
	}

	log.Printf("Nodo %d sincronizó %d sesiones desde nodo %d",
		s.nodeID, len(req.Sessions), req.NodeID)

	return c.JSON(fiber.Map{
		"message":  "Sync completed",
		"synced":   len(req.Songs),
		"sessions": len(req.Sessions),
		"users":    len(req.Users),
		"node_id":  s.nodeID,
	})
}

// Usar Upsert (crear o actualizar)

func (s *Server) initialSyncAfterJoin() {
	// Intentar encontrar líder inmediatamente
	leaderID := s.discoverLeaderByScanning()
	if leaderID == 0 {
		// Si no hay líder aún, esperar a que se establezca uno
		leaderID = s.waitForLeader(30 * time.Second)
	}

	// Iniciar loop de sincronización periódica
	s.continuousSyncWithLeader()
}

// waitForLeader espera hasta encontrar un líder o timeout
func (s *Server) waitForLeader(timeout time.Duration) int {
	deadline := time.After(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			log.Printf("Nodo %d: timeout esperando líder", s.nodeID)
			return 0
		case <-ticker.C:
			s.mu.RLock()
			leaderID := s.leaderID
			isLeader := s.isLeader
			s.mu.RUnlock()

			if leaderID > 0 && !isLeader {
				log.Printf("Nodo %d detectó líder %d", s.nodeID, leaderID)
				return leaderID
			}
		}
	}
}

// continuousSyncWithLeader realiza sincronización periódica con el líder
func (s *Server) continuousSyncWithLeader() {
	syncTicker := time.NewTicker(10 * time.Second)
	defer syncTicker.Stop()

	for range syncTicker.C {
		s.mu.RLock()
		leaderID := s.leaderID
		isLeader := s.isLeader
		s.mu.RUnlock()

		// Solo los followers necesitan sincronizarse periódicamente
		if !isLeader && leaderID > 0 {
			log.Printf("Nodo %d realiza sincronización periódica con líder %d", s.nodeID, leaderID)
			s.syncDataFromLeader()
		}
	}
}

func (s *Server) songsSnapshotHandler(c *fiber.Ctx) error {
	// Solo el líder debería servir este endpoint de verdad
	s.mu.RLock()
	isLeader := s.isLeader
	lastAppliedIndex, _ := s.opLog.GetLastOperationInfo()
	s.mu.RUnlock()
	if !isLeader {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":     "Solo el líder puede proveer snapshot",
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

	// Obtener todas las sesiones activas
	var sessions []models.Session
	if err := s.db.Find(&sessions).Error; err != nil {
		log.Printf("Error obteniendo snapshot de Sessions: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error fetching sessions from DB",
		})
	}

	// Obtener todos los usuarios
	var users []models.User
	if err := s.db.Find(&users).Error; err != nil {
		log.Printf("Error obteniendo snapshot de Users: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error fetching users from DB",
		})
	}

	// Obtener todos los operation logs (historial de operaciones)
	var operationLogs []models.OperationLog
	if err := s.db.Order("index asc").Find(&operationLogs).Error; err != nil {
		log.Printf("Error obteniendo snapshot de OperationLogs: %v", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Error fetching operation logs from DB",
		})
	}

	// Convertir sesiones al formato de replicación
	var replicationSessions []ReplicationSession
	for _, sess := range sessions {
		replicationSessions = append(replicationSessions, ReplicationSession{
			ID:           sess.ID,
			UserID:       sess.UserID,
			IPAddress:    sess.IPAddress,
			UserAgent:    sess.UserAgent,
			LastActivity: sess.LastActivity,
			ExpiresAt:    sess.ExpiresAt,
		})
	}

	// Convertir usuarios al formato de replicación
	var replicationUsers []ReplicationUser
	for _, user := range users {
		replicationUsers = append(replicationUsers, ReplicationUser{
			ID:        user.ID,
			Username:  user.Username,
			Password:  user.Password,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
			UpdatedAt: user.UpdatedAt,
		})
	}

	return c.JSON(fiber.Map{
		"songs":              songs,
		"sessions":           replicationSessions,
		"users":              replicationUsers,
		"operation_logs":     operationLogs,
		"last_applied_index": lastAppliedIndex,
		"count":              len(songs),
		"node_id":            s.nodeID,
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

// replicateSessionToFollowers replica una nueva sesión a todos los followers
func (s *Server) replicateSessionToFollowers(session models.Session, minAcks int) bool {
	log.Print("ESTOY REPLICANDO LA SESION")

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
		return true
	}

	if totalFollowers < minAcks {
		minAcks = totalFollowers
	}

	var wg sync.WaitGroup
	ackChan := make(chan bool, totalFollowers)

	for _, nodeID := range targetNodes {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			type SyncRequest struct {
				Sessions []ReplicationSession `json:"sessions"`
				NodeID   int                  `json:"node_id"`
			}

			reqBody := SyncRequest{
				Sessions: []ReplicationSession{{
					ID:           session.ID,
					UserID:       session.UserID,
					IPAddress:    session.IPAddress,
					UserAgent:    session.UserAgent,
					LastActivity: session.LastActivity,
					ExpiresAt:    session.ExpiresAt,
				}},
				NodeID: s.nodeID,
			}

			jsonData, _ := json.Marshal(reqBody)
			url := fmt.Sprintf("http://backend%d:3003/internal/sync", id)
			client := &http.Client{Timeout: 2 * time.Second}
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

	return acksReceived >= minAcks
}

func (s *Server) replicateUserToFollowers(user models.User, minAcks int) bool {
	followers := s.getAllNodeIDs()
	var targetNodes []int
	for _, id := range followers {
		if id != s.nodeID {
			targetNodes = append(targetNodes, id)
		}
	}

	if len(targetNodes) == 0 {
		return true
	}

	var wg sync.WaitGroup
	ackChan := make(chan bool, len(targetNodes))

	for _, nodeID := range targetNodes {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			type SyncRequest struct {
				Users  []ReplicationUser `json:"users"`
				NodeID int               `json:"node_id"`
			}

			reqBody := SyncRequest{
				Users: []ReplicationUser{{
					ID:        user.ID,
					Username:  user.Username,
					Password:  user.Password,
					Role:      user.Role,
					CreatedAt: user.CreatedAt,
					UpdatedAt: user.UpdatedAt,
				}},
				NodeID: s.nodeID,
			}

			jsonData, _ := json.Marshal(reqBody)
			url := fmt.Sprintf("http://backend%d:3003/internal/sync", id)
			client := &http.Client{Timeout: 2 * time.Second}
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
	return acksReceived >= minAcks
}

func (s *Server) triggerReconciliation(targetNodeID int) {
	log.Printf("CONFLICTO RESOLUTION: Iniciando reconciliación con el líder ganador %d...", targetNodeID)

	// 1. Recolectar datos locales
	var songs []models.Song
	if err := s.db.Find(&songs).Error; err != nil {
		log.Printf("Error leyendo canciones para reconciliación: %v", err)
		return
	}

	var users []models.User
	if err := s.db.Find(&users).Error; err != nil {
		log.Printf("Error leyendo usuarios para reconciliación: %v", err)
		return
	}

	var sessions []models.Session
	if err := s.db.Find(&sessions).Error; err != nil {
		log.Printf("Error leyendo sesiones para reconciliación: %v", err)
		return
	}

	// 2. Convertir a estructuras de transporte (para evitar problemas con preloading/json)
	var repUsers []ReplicationUser
	for _, u := range users {
		repUsers = append(repUsers, ReplicationUser{
			ID:        u.ID,
			Username:  u.Username,
			Password:  u.Password,
			Role:      u.Role,
			CreatedAt: u.CreatedAt,
			UpdatedAt: u.UpdatedAt,
		})
	}

	var repSessions []ReplicationSession
	for _, sess := range sessions {
		repSessions = append(repSessions, ReplicationSession{
			ID:           sess.ID,
			UserID:       sess.UserID,
			IPAddress:    sess.IPAddress,
			UserAgent:    sess.UserAgent,
			LastActivity: sess.LastActivity,
			ExpiresAt:    sess.ExpiresAt,
		})
	}

	payload := map[string]interface{}{
		"songs":    songs,
		"users":    repUsers,
		"sessions": repSessions,
		"node_id":  s.nodeID,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error marshalling reconciliation data: %v", err)
		return
	}

	// 3. Enviar al líder ganador
	url := fmt.Sprintf("%s/internal/reconcile", s.nodeURL(targetNodeID))
	client := &http.Client{Timeout: 30 * time.Second} // Timeout largo para transferencia de datos
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("Error enviando datos de reconciliación: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		log.Printf("Reconciliación enviada con éxito al nodo %d. Mis datos no se perderán.", targetNodeID)
	} else {
		log.Printf("Fallo en reconciliación, status: %d", resp.StatusCode)
	}
}

// reconcileHandler recibe datos de un nodo perdedor y los integra
func (s *Server) reconcileHandler(c *fiber.Ctx) error {
	type ReconcileRequest struct {
		Songs      []models.Song        `json:"songs"`
		Sessions   []ReplicationSession `json:"sessions"`
		Users      []ReplicationUser    `json:"users"`
		NodeID     int                  `json:"node_id"`
		NodePrefix string               `json:"node_prefix"`
	}

	var req ReconcileRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	log.Printf("Procesando RECONCILIACIÓN del nodo %d (%d songs, %d users, %d sessions)",
		req.NodeID, len(req.Songs), len(req.Users), len(req.Sessions))

	// Procesar Canciones evitando colisiones de IDs
	for _, incomingSong := range req.Songs {
		// PASO 1: Verificar si el ID ya existe localmente
		var existingSong models.Song
		result := s.db.Where("id = ?", incomingSong.ID).First(&existingSong)

		if result.Error != nil && result.Error == gorm.ErrRecordNotFound {
			// El ID no existe localmente → CREAR normalmente
			if err := s.db.Create(&incomingSong).Error; err != nil {
				log.Printf("Error creando canción %d: %v", incomingSong.ID, err)
				continue
			}
			s.opLog.Append(OpCreate, incomingSong)

		} else if result.Error != nil {
			log.Printf("Error consultando canción %d: %v", incomingSong.ID, result.Error)
			continue

		} else {
			if s.isSameSong(existingSong, incomingSong) {
				log.Printf("Canción ID %d ya existe, actualizando si es necesario", incomingSong.ID)
				if !s.songsAreEqual(existingSong, incomingSong) {
					if err := s.db.Model(&existingSong).Updates(map[string]interface{}{
						"title":      incomingSong.Title,
						"artist":     incomingSong.Artist,
						"duration":   incomingSong.Duration,
						"file_path":  incomingSong.File,
						"album":      incomingSong.Album,
						"genre":      incomingSong.Genre,
						"updated_at": time.Now(),
					}).Error; err != nil {
						log.Printf("Error actualizando canción %d: %v", incomingSong.ID, err)
						continue
					}
					s.opLog.Append(OpUpdate, incomingSong)
				}
			} else {
				log.Printf("COLISIÓN DETECTADA: ID %d usado por canción '%s - %s' (local) vs '%s - %s' (remota)",
					incomingSong.ID, existingSong.Title, existingSong.Artist, incomingSong.Title, incomingSong.Artist)

				newID := s.getMaxIDAlternative("songs")
				log.Printf("Asignando nuevo ID %d a canción entrante '%s - %s'", newID, incomingSong.Title, incomingSong.Artist)

				incomingSong.ID = newID
				if err := s.db.Create(&incomingSong).Error; err != nil {
					log.Printf("Error creando canción con nuevo ID %d: %v", newID, err)
					continue
				}

				s.opLog.Append(OpCreate, incomingSong)
			}
		}
	}

	go func() {
		for _, song := range req.Songs {
			// Check if file exists locally
			filePath := filepath.Join("storage/songs", song.File)
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				log.Printf("Reconciliación: Descargando archivo faltante %s desde nodo perdedor %d", song.File, req.NodeID)
				// You need to implement/expose a method to download from a specific node ID
				s.downloadFileFromNode(song.File, req.NodeID)
			}
		}
	}()

	// Procesar Usuarios
	for _, incomingUser := range req.Users {
		var localUser models.User
		result := s.db.Where("id = ?", incomingUser.ID).First(&localUser)

		if result.Error != nil && result.Error == gorm.ErrRecordNotFound {
			// Crear usuario nuevo
			user := models.User{
				ID:        incomingUser.ID,
				Username:  incomingUser.Username,
				Password:  incomingUser.Password,
				Role:      incomingUser.Role,
				CreatedAt: incomingUser.CreatedAt,
				UpdatedAt: incomingUser.UpdatedAt,
			}
			if err := s.db.Create(&user).Error; err != nil {
				log.Printf("Error creando usuario %d: %v", incomingUser.ID, err)
				continue
			}
			s.opLog.AppendUser(OpCreateUser, user)

		} else if result.Error == nil {
			// Verificar si es el mismo usuario
			if s.isSameUser(localUser, incomingUser) {
				// Actualizar si hay cambios
				if localUser.UpdatedAt.Before(incomingUser.UpdatedAt) ||
					localUser.Username != incomingUser.Username ||
					localUser.Password != incomingUser.Password ||
					localUser.Role != incomingUser.Role {

					if err := s.db.Model(&localUser).Updates(map[string]interface{}{
						"username":   incomingUser.Username,
						"password":   incomingUser.Password,
						"role":       incomingUser.Role,
						"updated_at": time.Now(),
					}).Error; err != nil {
						log.Printf("Error actualizando usuario %d: %v", localUser.ID, err)
						continue
					}
					s.opLog.AppendUser(OnUpdateUser, models.User{
						ID:       localUser.ID,
						Username: incomingUser.Username,
						Password: incomingUser.Password,
						Role:     incomingUser.Role,
					})
				}
			} else {
				// Colisión de ID de usuario
				log.Printf("COLISIÓN DETECTADA en usuario: ID %d usado por '%s' (local) vs '%s' (remota)",
					incomingUser.ID, localUser.Username, incomingUser.Username)

				newID := s.getMaxIDAlternative("users")

				user := models.User{
					ID:        newID,
					Username:  incomingUser.Username,
					Password:  incomingUser.Password,
					Role:      incomingUser.Role,
					CreatedAt: incomingUser.CreatedAt,
					UpdatedAt: incomingUser.UpdatedAt,
				}

				if err := s.db.Create(&user).Error; err != nil {
					log.Printf("Error creando usuario con nuevo ID %d: %v", newID, err)
					continue
				}

				s.opLog.AppendUser(OpCreateUser, user)
			}
		}
	}

	// Procesar Sesiones
	for _, incomingSession := range req.Sessions {
		var localSession models.Session
		result := s.db.Where("id = ?", incomingSession.ID).First(&localSession)

		if result.Error != nil && result.Error == gorm.ErrRecordNotFound {
			session := models.Session{
				ID:           incomingSession.ID,
				UserID:       incomingSession.UserID,
				IPAddress:    incomingSession.IPAddress,
				UserAgent:    incomingSession.UserAgent,
				LastActivity: incomingSession.LastActivity,
				ExpiresAt:    incomingSession.ExpiresAt,
			}
			if err := s.db.Create(&session).Error; err != nil {
				log.Printf("Error creando sesión %s: %v", incomingSession.ID, err)
				continue
			}
			s.opLog.AppendSession(OpCreateSession, session)

		} else if result.Error == nil {
			// Para sesiones, podemos simplemente actualizar ya que son temporales
			if err := s.db.Model(&localSession).Updates(map[string]interface{}{
				"last_activity": incomingSession.LastActivity,
				"expires_at":    incomingSession.ExpiresAt,
				"ip_address":    incomingSession.IPAddress,
			}).Error; err != nil {
				log.Printf("Error actualizando sesión %s: %v", incomingSession.ID, err)
				continue
			}
			s.opLog.AppendSession(OnUpdateSession, models.Session{
				ID:           localSession.ID,
				UserID:       localSession.UserID,
				LastActivity: incomingSession.LastActivity,
				ExpiresAt:    incomingSession.ExpiresAt,
			})
		}
	}

	log.Printf("Reconciliación completada. IDs duplicados resueltos.")

	go s.PropagateSnapshotToFollowers()

	return c.SendStatus(fiber.StatusOK)
}

func (s *Server) applyOperations(ops []Operation) {
	for _, op := range ops {
		switch op.Type {
		case OpCreate:
			var existingSong models.Song
			result := s.db.Where("id = ?", op.Data.ID).First(&existingSong)

			if result.Error != nil && result.Error == gorm.ErrRecordNotFound {
				s.db.Where("id > ?", op.Data.ID).Limit(50).Delete(&models.Song{})
				log.Printf("Eliminando canciones a partir del ID %d", op.Data.ID)
			}

			if err := s.db.Create(&op.Data).Error; err != nil {
				log.Printf("Error creando canción %d: %v", op.Data.ID, err)
				continue
			}

		case OpCreateUser:
			var existingUser models.User
			result := s.db.Where("id = ?", op.UserData.ID).First(&existingUser)

			if result.Error != nil && result.Error == gorm.ErrRecordNotFound {
				s.db.Where("id > ?", op.UserData.ID).Limit(50).Delete(&models.User{})
				log.Printf("Eliminando usuarios a partir del ID %d", op.UserData.ID)
			}

			if err := s.db.Create(&op.UserData).Error; err != nil {
				log.Printf("Error creando usuario %d: %v", op.UserData.ID, err)
				continue
			}
		case OpCreateSession:
			var existingSession models.Session
			result := s.db.Where("id = ?", op.SessionData.ID).First(&existingSession)

			if result.Error != nil && result.Error == gorm.ErrRecordNotFound {
				log.Printf("YA EXISTE UNA SESION con ID %s que corresponde al usuario con ID %d", existingSession.ID, existingSession.UserID)
				s.db.Where("id > ?", op.SessionData.ID).Limit(50).Delete(&models.Session{})
				log.Printf("Eliminando Session a partir del ID %s", op.SessionData.ID)
			}

			if err := s.db.Create(&op.SessionData).Error; err != nil {
				log.Printf("Error creando sesión %s: %v", op.SessionData.ID, err)
				continue
			}
		}

		jsonData, _ := json.Marshal(op.Data)
		var userJson []byte
		var sessionJson []byte

		if op.UserData != nil {
			userJson, _ = json.Marshal(op.UserData)
		}
		if op.SessionData != nil {
			sessionJson, _ = json.Marshal(op.SessionData)
		}

		opLogEntry := models.OperationLog{
			ID:          uint(op.Index), // Forzamos que el ID sea igual al Index para consistencia total
			Index:       op.Index,       // El Index original del líder
			Type:        string(op.Type),
			Data:        jsonData,
			UserData:    userJson,
			SessionData: sessionJson,
			Timestamp:   op.Timestamp,
		}

		// Usar Upsert (OnConflict DoNothing) para insertar en operation_logs
		// Esto garantiza que si ya tenemos este log, no se duplique ni falle
		if err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "index"}}, // Clave única es el Index
			DoNothing: true,                             // Si ya existe, es idéntico (logs inmutables)
		}).Create(&opLogEntry).Error; err != nil {
			log.Printf("Error replicando OpLog %d: %v", op.Index, err)
		}

	}

	go func() {
		s.db.Exec("SELECT setval('songs_id_seq', COALESCE((SELECT MAX(id) FROM songs), 1))")
		s.db.Exec("SELECT setval('users_id_seq', COALESCE((SELECT MAX(id) FROM users), 1))")
		// IMPORTANTE: operation_logs
		s.db.Exec("SELECT setval('operation_logs_id_seq', COALESCE((SELECT MAX(id) FROM operation_logs), 1))")
	}()

	// Marcar como sincronizado después de aplicar las operaciones
	if len(ops) > 0 {
		s.syncMutex.Lock()
		s.isSyncedWithLeader = true
		s.syncMutex.Unlock()
	}
}

func (s *Server) getMaxIDAlternative(tableName string) uint {
	var maxID uint

	// Método alternativo usando GORM con modelos
	switch tableName {
	case "songs":
		s.db.Model(&models.Song{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID)
	case "users":
		s.db.Model(&models.User{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID)
	default:
		log.Printf("Tabla %s no reconocida, usando valor por defecto", tableName)
		var songsMax, usersMax uint
		s.db.Model(&models.Song{}).Select("COALESCE(MAX(id), 0)").Scan(&songsMax)
		s.db.Model(&models.User{}).Select("COALESCE(MAX(id), 0)").Scan(&usersMax)
		maxID = max(songsMax, usersMax) + 1
	}

	return maxID + 1
}

// Funciones para comparar registros
func (s *Server) isSameSong(a, b models.Song) bool {
	return strings.EqualFold(a.Title, b.Title) &&
		strings.EqualFold(a.Artist, b.Artist) &&
		a.Duration == b.Duration
}

func (s *Server) songsAreEqual(a, b models.Song) bool {
	return a.Title == b.Title &&
		a.Artist == b.Artist &&
		a.Duration == b.Duration &&
		a.File == b.File &&
		a.Album == b.Album &&
		a.Genre == b.Genre
}

func (s *Server) isSameUser(a models.User, b ReplicationUser) bool {
	return strings.EqualFold(a.Username, b.Username)
}

func max(a, b uint) uint {
	if a > b {
		return a
	}
	return b
}
