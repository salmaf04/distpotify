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

	// Actualizar las secuencias de auto-increment después de insertar datos
	// Esto asegura que el siguiente INSERT use un ID mayor al máximo existente
	if len(result.Songs) > 0 {
		if err := tx.Exec("SELECT setval('songs_id_seq', COALESCE((SELECT MAX(id) FROM songs), 0) + 1)").Error; err != nil {
			log.Printf("Error actualizando secuencia songs_id_seq: %v", err)
		}
	}

	if len(result.Users) > 0 {
		if err := tx.Exec("SELECT setval('users_id_seq', COALESCE((SELECT MAX(id) FROM users), 0) + 1)").Error; err != nil {
			log.Printf("Error actualizando secuencia users_id_seq: %v", err)
		}
	}

	// Commit de la transacción
	if err := tx.Commit().Error; err != nil {
		log.Printf("Error haciendo commit de sync desde líder: %v", err)
		return
	}

	// Actualizar el lastAppliedIndex local
	s.mu.Lock()
	s.lastAppliedIndex = result.LastAppliedIndex
	s.mu.Unlock()

	log.Printf("Nodo %d sincronizó correctamente: %d canciones, %d sesiones, %d usuarios, %d operation logs. LastAppliedIndex=%d",
		s.nodeID, len(result.Songs), len(result.Sessions), len(result.Users), len(result.OperationLogs), result.LastAppliedIndex)

	// Marcar como sincronizado
	s.syncMutex.Lock()
	s.isSyncedWithLeader = true
	s.syncMutex.Unlock()
}

func (s *Server) applyOperations(ops []Operation) {
	for _, op := range ops {
		switch op.Type {
		case OpCreate:
			// CORRECCIÓN: Usar Upsert aquí también
			s.db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				DoUpdates: clause.AssignmentColumns([]string{"title", "artist", "duration", "file_path"}),
			}).Create(&op.Data)

		case OpCreateUser:
			if op.UserData != nil {
				s.db.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "id"}},
					DoUpdates: clause.AssignmentColumns([]string{"username", "password", "role"}),
				}).Create(op.UserData)
			}
		}

		jsonData, _ := json.Marshal(op.Data)
		var userJson []byte
		if op.UserData != nil {
			userJson, _ = json.Marshal(op.UserData)
		}

		opLogEntry := models.OperationLog{
			ID:        uint(op.Index), // Forzamos que el ID sea igual al Index para consistencia total
			Index:     op.Index,       // El Index original del líder
			Type:      string(op.Type),
			Data:      jsonData,
			UserData:  userJson,
			Timestamp: op.Timestamp,
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

	// Marcar como sincronizado después de aplicar las operaciones
	if len(ops) > 0 {
		s.syncMutex.Lock()
		s.isSyncedWithLeader = true
		s.syncMutex.Unlock()
	}
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

	// Procesar sincronización de canciones
	for _, song := range req.Songs {
		// ERROR ORIGINAL: song.ID = 0  <-- BORRAR ESTA LÍNEA
		// Debemos respetar el ID que viene del líder

		// CORRECCIÓN: Usar Upsert (OnConflict)
		// Si el ID ya existe, no hacemos nada (DoNothing) o actualizamos.
		// Como es un log inmutable de creación, DoNothing suele bastar,
		// pero UpdateAll asegura consistencia si hubo cambios.
		result := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},                                                                    // Conflicto por ID
			DoUpdates: clause.AssignmentColumns([]string{"title", "artist", "duration", "file_path", "album", "genre"}), // Actualizar campos
		}).Create(&song)

		if result.Error != nil {
			log.Printf("Error sincronizando canción %s: %v", song.Title, result.Error)
		}
	}

	log.Printf("Nodo %d sincronizó %d canciones desde nodo %d",
		s.nodeID, len(req.Songs), req.NodeID)

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
	}

	log.Printf("Nodo %d sincronizó %d sesiones desde nodo %d",
		s.nodeID, len(req.Sessions), req.NodeID)

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
