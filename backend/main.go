package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"distributed-systems-project/handlers"
	"distributed-systems-project/middleware"
	"distributed-systems-project/models"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Server struct {
	app                *fiber.App
	db                 *gorm.DB
	sqlDB              *sql.DB
	nodeID             int
	apiPort            int
	isLeader           bool
	leaderID           int
	mu                 sync.RWMutex
	songHandler        *handlers.SongHandler
	authHandler        *handlers.AuthHandler
	streamHandler      *handlers.StreamHandler
	opLog              *OpLog
	isSyncedWithLeader bool         // Indica si está completamente sincronizado con el líder
	syncMutex          sync.RWMutex // Mutex separado para sincronización
}

const (
	ElectionTimeout     = 3 * time.Second
	HeartbeatInterval   = 5 * time.Second
	CoordinationTimeout = 2 * time.Second
)

// === NUEVO: Mensajes del protocolo Bully ===
type ElectionMessage struct {
	Type          string `json:"type"` // "ELECTION", "ANSWER", "COORDINATOR"
	NodeID        int    `json:"node_id"`
	LastIndex     int64  `json:"last_index"`     // Índice del último op log
	LastTimestamp int64  `json:"last_timestamp"` // Timestamp del último op log para desempates
}

func (s *Server) nodeURL(nodeID int) string {
	return fmt.Sprintf("http://backend%d:3003", nodeID)
}

func getDBConfig(nodeID int) string {
	dbHost := fmt.Sprintf("db%d", nodeID)

	return fmt.Sprintf(
		"host=%s user=music password=music dbname=musicdb port=5432 sslmode=disable",
		dbHost,
	)
}

func NewServer(nodeID, apiPort int) *Server {
	// Obtener string de conexión
	dsn := getDBConfig(nodeID)

	// Conectar a PostgreSQL usando GORM
	gormDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Error conectando a DB: %v", err)
	}

	// Obtener la conexión SQL subyacente
	sqlDB, err := gormDB.DB()
	if err != nil {
		log.Fatalf("Error obteniendo conexión SQL: %v", err)
	}

	// Configurar conexión
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Verificar conexión
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		log.Fatalf("DB no responde: %v", err)
	}

	// Migrar modelos
	err = gormDB.AutoMigrate(
		&models.Song{},
		&models.User{},
		&models.OperationLog{},
		&models.Session{},
	)
	if err != nil {
		log.Printf("Error en migración: %v", err)
	}

	// Crear handler de canciones
	songHandler := &handlers.SongHandler{DB: gormDB}
	streamHandler := &handlers.StreamHandler{DB: gormDB, SongsDir: "storage/songs"}
	opLog := NewOpLog(gormDB)

	authHandler := &handlers.AuthHandler{
		DB: gormDB,
		OnSessionCreated: func(session models.Session) {
			// Callback: Cuando alguien se loguea, escribimos en el OpLog
			// Necesitamos un nuevo tipo de operación OpCreateSession
			opLog.AppendSession(OpCreateSession, session)
		},
	}

	// Crear app Fiber
	app := fiber.New(fiber.Config{
		AppName:       fmt.Sprintf("Music-Replica-%d", nodeID),
		CaseSensitive: true,
		StrictRouting: true,
		BodyLimit:     100 * 1024 * 1024,
		ReadTimeout:   60 * time.Second,
		WriteTimeout:  60 * time.Second,
	})

	log.Printf("HOLA AQUI ESTOY INICIANDO")

	// Middleware
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "http://localhost:5173", // Origen específico
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Content-Type,Authorization,X-Data-Version,X-Node-ID",
		AllowCredentials: true,         // IMPORTANTE: true cuando usas credentials: 'include'
		ExposeHeaders:    "Set-Cookie", // Necesario para cookies
	}))

	app.Use(logger.New(logger.Config{
		Format: "${time} [${ip}]:${port} ${status} - ${method} ${path}\n",
	}))

	server := &Server{
		app:           app,
		db:            gormDB,
		sqlDB:         sqlDB,
		nodeID:        nodeID,
		apiPort:       apiPort,
		isLeader:      false,
		leaderID:      0,
		songHandler:   songHandler,
		authHandler:   authHandler,
		streamHandler: streamHandler,
		opLog:         opLog,
	}

	log.Printf("Me reinicie")

	server.setupRoutes()
	go server.bootstrapLeadership()
	go server.startHeartbeatMonitor()
	go server.initialSyncAfterJoin()

	return server
}

func (s *Server) setupRoutes() {
	// Health check para proxy
	s.app.Get("/health", s.healthHandler)
	s.app.Get("/cluster", s.clusterHandler)

	// API de canciones y autenticación
	api := s.app.Group("/api")

	s.app.Post("/auth/register", s.registerHandler)
	s.app.Post("/auth/login", s.loginHandler)

	// Rutas de canciones con redirección automática
	api.Post("/songs/upload", middleware.Protected(s.db), middleware.AdminOnly(), s.uploadSongHandler)

	api.Get("/songs", s.getSongsHandler)           // Cualquier réplica
	api.Get("/songs/:id", s.getSongByIDHandler)    // Cualquier réplica
	api.Get("/songs/search", s.searchSongsHandler) // Cualquier réplica
	api.Get("/songs/filter", s.filterSongsHandler) // Cualquier réplica
	s.app.Get("/internal/sync/delta", s.deltaSyncHandler)

	s.app.Post("/internal/election", s.electionHandler)
	s.app.Get("/internal/songs/snapshot", s.songsSnapshotHandler)

	// Sincronización (para uso interno)
	s.app.Post("/internal/sync", s.syncHandler)

	// Replicación de archivos MP3
	s.app.Post("/internal/receive-file", s.receiveFileHandler)
	s.app.Get("/internal/download-file", s.downloadFileHandler)

	api.Get("/songs/:id/stream", s.streamSongHandler)
	s.app.Post("/internal/reconcile", s.reconcileHandler)

	// Endpoint para verificar estado de sincronización
	s.app.Get("/internal/sync-status", s.syncStatusHandler)
}

// syncStatusHandler retorna el estado actual de sincronización del nodo
func (s *Server) syncStatusHandler(c *fiber.Ctx) error {
	s.syncMutex.RLock()
	isSynced := s.isSyncedWithLeader
	s.syncMutex.RUnlock()

	s.mu.RLock()
	lastIndex, timestamp := s.opLog.GetLastOperationInfo()
	s.mu.RUnlock()

	return c.JSON(fiber.Map{
		"node_id":            s.nodeID,
		"is_leader":          s.isLeader,
		"leader_id":          s.leaderID,
		"synced_with_leader": isSynced,
		"last_index":         lastIndex,
		"last_timestamp":     timestamp,
	})
}

func (s *Server) electionHandler(c *fiber.Ctx) error {
	var msg ElectionMessage
	if err := c.BodyParser(&msg); err != nil {
		return c.Status(400).SendString("bad request")
	}

	switch msg.Type {
	case "ELECTION":
		log.Printf("Nodo %d recibió ELECTION de nodo %d (Index: %d, Timestamp: %d)",
			s.nodeID, msg.NodeID, msg.LastIndex, msg.LastTimestamp)

		myIndex, myTimestamp := s.opLog.GetLastOperationInfo()
		myID := s.nodeID

		// Comparación mejorada: Index > Timestamp > NodeID
		amIBetter := false
		if myIndex > msg.LastIndex {
			log.Printf("[BULLY] SOY MEJOR QUE TU Nodo con operaciones")
			// Yo tengo más operaciones
			amIBetter = true
		} else if myIndex == msg.LastIndex {
			// Mismo número de operaciones, comparar por timestamp
			if myTimestamp > msg.LastTimestamp {
				log.Printf("[BULLY] SOY MEJOR QUE TU Nodo con timestamp")
				amIBetter = true
			} else if myTimestamp == msg.LastTimestamp && myID > msg.NodeID {
				log.Printf("[BULLY] SOY MEJOR QUE TU Nodo con id %d vs %d", myID, msg.NodeID)
				// Mismo timestamp, usar nodeID como desempate
				amIBetter = true
			}
		}

		if amIBetter {
			log.Printf("Nodo %d es mejor candidato (Index: %d > %d OR Timestamp más reciente)",
				s.nodeID, myIndex, msg.LastIndex)
			// Responder ANSWER para detener al otro candidato
			resp := ElectionMessage{
				Type:          "ANSWER",
				NodeID:        myID,
				LastIndex:     myIndex,
				LastTimestamp: myTimestamp,
			}
			c.JSON(resp)

			// Iniciar mi propia elección si soy mejor y no soy líder
			go s.startLeaderElection()
		}

	case "COORDINATOR":
		// === NUEVA LÓGICA DE RECONCILIACIÓN ===
		s.mu.RLock()
		isCurrentLeader := s.isLeader
		s.mu.RUnlock()

		// Si yo soy líder y recibo un aviso de OTRO líder, hay conflicto
		if isCurrentLeader && msg.NodeID != s.nodeID {
			myIndex, myTimestamp := s.opLog.GetLastOperationInfo()

			amIBetter := false
			// Usamos la misma lógica de "quién es mejor" que en ELECTION
			if myIndex > msg.LastIndex {
				amIBetter = true
			} else if myIndex == msg.LastIndex {
				if myTimestamp > msg.LastTimestamp {
					amIBetter = true
				} else if myTimestamp == msg.LastTimestamp && s.nodeID > msg.NodeID {
					amIBetter = true
				}
			}

			if amIBetter {
				log.Printf("CONFLICTO: Ignoro al líder %d porque yo (%d) soy mejor. Me impongo.", msg.NodeID, s.nodeID)
				// Reenviamos nuestro COORDINATOR para que el otro se entere y renuncie él
				go s.broadcastCoordinator()
				return c.SendStatus(fiber.StatusOK)
			} else {
				log.Printf("CONFLICTO: El líder %d es mejor que yo. Renuncio al liderazgo.", msg.NodeID)
				s.mu.RLock()
				s.isLeader = false
				s.mu.RUnlock()
				s.triggerReconciliation(msg.NodeID)
				// Dejamos continuar el código para que acepte al nuevo líder abajo
			}
		}
		// === FIN NUEVA LÓGICA ===

		// Lógica original de aceptación
		s.mu.Lock()
		s.leaderID = msg.NodeID
		s.isLeader = (msg.NodeID == s.nodeID)
		log.Printf("Nodo %d acepta a %d como líder", s.nodeID, msg.NodeID)
		s.mu.Unlock()
	}

	return c.SendStatus(fiber.StatusOK)
}

// === Detección de fallo del líder (Heartbeat) ===
func (s *Server) startHeartbeatMonitor() {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.RLock()
		leaderID := s.leaderID
		isLeader := s.isLeader
		s.mu.RUnlock()

		if isLeader {
			// El líder solo hace log
			log.Printf("Líder %d activo - heartbeat en el hearbeat de verdad", s.nodeID)
			go s.broadcastCoordinator()
			continue
		}

		if leaderID <= 0 {
			continue
		}

		// Verificar si el líder responde
		url := s.nodeURL(leaderID) + "/health"
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(url)

		if err != nil || resp.StatusCode != http.StatusOK {
			log.Printf("Líder %d no responde! Iniciando elección...", leaderID)
			s.mu.Lock()
			s.leaderID = -1
			s.isLeader = false
			s.mu.Unlock()

			time.Sleep(time.Duration(rand.Intn(500)) * time.Millisecond) // 0-500ms de espera aleatoria
			go s.startLeaderElection()
		} else {
			log.Printf("Nodo %d activo - recibe heartbeat de líder %d", s.nodeID, leaderID)
			resp.Body.Close()
		}
	}
}

// === Utilidades ===

// Handler para upload de canción con redirección automática
func (s *Server) uploadSongHandler(c *fiber.Ctx) error {
	s.mu.RLock()
	isLeader := s.isLeader
	leaderID := s.leaderID
	s.mu.RUnlock()

	if !isLeader {
		// Redirigir al líder
		return c.Status(fiber.StatusTemporaryRedirect).JSON(fiber.Map{
			"error":        "No soy el líder para operaciones de escritura",
			"redirect_to":  fmt.Sprintf("http://backend%d:3003/api/songs/upload", leaderID),
			"current_node": s.nodeID,
			"leader":       leaderID,
			"action":       "redirect_to_leader",
		})
	}

	// Soy el líder, procesar la subida
	result := s.songHandler.UploadSong(c)

	// Si el upload fue exitoso, registrar en el operation log y replicar
	if c.Response().StatusCode() == fiber.StatusCreated {
		// Obtener la canción que se acaba de insertar desde el contexto
		insertedSong, ok := c.Locals("inserted_song").(*models.Song)
		if ok && insertedSong != nil {
			// Registrar la operación en el oplog
			newIndex := s.opLog.Append(OpCreate, *insertedSong)

			log.Printf("Canción insertada y registrada en oplog: ID=%d, Index=%d", insertedSong.ID, newIndex)

			// Replicar con followers
			success := s.replicateToFollowers(*insertedSong, 2)
			if !success {
				log.Printf("ADVERTENCIA: No se alcanzó el quórum de replicación deseado")
			}
		}

		// Obtener el nombre del archivo que se acaba de guardar
		_, err := c.FormFile("file")
		if err == nil {
			artist := c.FormValue("artist")
			title := c.FormValue("title")

			// Construir el nombre del archivo como se hace en save_file.go
			safeArtist := sanitizeFileName(artist)
			safeTitle := sanitizeFileName(title)
			filename := fmt.Sprintf("%s-%s.mp3", safeArtist, safeTitle)
			filePath := filepath.Join("storage/songs", filename)

			// Replicar el archivo a los followers
			go s.ReplicateFilesToFollowers(filename, filePath)

			log.Printf("Nodo %d (líder): Iniciando replicación del archivo %s", s.nodeID, filename)
		}
	}

	return result
}

func (s *Server) deltaSyncHandler(c *fiber.Ctx) error {
	sinceIndex, _ := strconv.ParseInt(c.Query("since"), 10, 64)

	ops, ok := s.opLog.GetSince(sinceIndex)
	if !ok {
		// El nodo está muy desactualizado (el log ya rotó)
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{
			"error": "Log truncated, full snapshot required",
		})
	}

	return c.JSON(fiber.Map{
		"operations": ops,
		"count":      len(ops),
	})
}

// Handler para obtener todas las canciones (cualquier réplica)
func (s *Server) getSongsHandler(c *fiber.Ctx) error {
	// Lecturas pueden ser atendidas por cualquier réplica
	return s.songHandler.GetAllSongs(c)
}

// Handler para obtener canción por ID
func (s *Server) getSongByIDHandler(c *fiber.Ctx) error {
	return s.songHandler.GetSongByID(c)
}

// Handler para búsqueda de canciones
func (s *Server) searchSongsHandler(c *fiber.Ctx) error {
	return s.songHandler.GetSongsSearch(c)
}

// Handler para filtrado de canciones
func (s *Server) filterSongsHandler(c *fiber.Ctx) error {
	return s.songHandler.GetSongs(c)
}

func (s *Server) streamSongHandler(c *fiber.Ctx) error {
	log.Printf("AQUI")
	return s.streamHandler.StreamSong(c)
}

func (s *Server) registerHandler(c *fiber.Ctx) error {
	log.Printf("ENTRANDO AL REGISTER")

	s.mu.RLock()
	isLeader := s.isLeader
	leaderID := s.leaderID
	s.mu.RUnlock()

	if !isLeader {
		return c.Status(fiber.StatusTemporaryRedirect).JSON(fiber.Map{
			"error":       "Write operations must go to leader",
			"redirect_to": fmt.Sprintf("http://backend%d:3003/api/auth/register", leaderID),
			"leader":      leaderID,
		})
	}

	var input handlers.RegisterInput
	if err := c.BodyParser(&input); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid input"})
	}

	user, err := s.authHandler.CreateUser(input)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	// Replicar
	s.opLog.AppendUser(OpCreateUser, *user)
	go s.replicateUserToFollowers(*user, 2)

	return c.Status(fiber.StatusCreated).JSON(user)
}

func (s *Server) loginHandler(c *fiber.Ctx) error {
	log.Printf("ENTRANDO AL LOGIN")

	s.mu.RLock()
	isLeader := s.isLeader
	leaderID := s.leaderID
	s.mu.RUnlock()

	if !isLeader {
		return c.Status(fiber.StatusTemporaryRedirect).JSON(fiber.Map{
			"error":       "Auth operations must go to leader",
			"redirect_to": fmt.Sprintf("http://backend%d:3003/api/auth/login", leaderID),
			"leader":      leaderID,
		})
	}

	user, session, err := s.authHandler.Login(c)
	if err != nil {
		switch err.Error() {
		case "user already has an active session":
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "User already has an active session",
			})
		default:
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
		}
	}

	// Replicar sesión a followers
	s.opLog.AppendSession(OpCreateSession, *session)
	go s.replicateSessionToFollowers(*session, 2)

	// Crear cookie
	c.Cookie(&fiber.Cookie{
		Name:     "session_id",
		Value:    session.ID,
		Expires:  session.ExpiresAt,
		HTTPOnly: true,
		Secure:   false, // Cambiar a true en producción con HTTPS
		SameSite: "Strict",
	})

	// Limpiar datos sensibles del usuario
	userResponse := map[string]interface{}{
		"id":         user.ID,
		"username":   user.Username,
		"role":       user.Role,
		"created_at": user.CreatedAt,
	}

	return c.JSON(fiber.Map{
		"message": "Login successful",
		"user":    userResponse,
		"session": map[string]interface{}{
			"expires_at": session.ExpiresAt,
		},
	})
}

// Health handler
func (s *Server) healthHandler(c *fiber.Ctx) error {
	// Verificar conexión a DB
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dbOK := true
	if err := s.sqlDB.PingContext(ctx); err != nil {
		dbOK = false
	}

	log.Printf("SE esta contactando mediante health")

	return c.JSON(fiber.Map{
		"status":    "healthy",
		"node_id":   s.nodeID,
		"leader_id": s.leaderID,
		"is_leader": s.isLeader,
		"database":  dbOK,
		"timestamp": time.Now().Unix(),
		"service":   "music-api",
	})
}

// Cluster handler
func (s *Server) clusterHandler(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"node_id":   s.nodeID,
		"is_leader": s.isLeader,
		"leader_id": s.leaderID,
		"api_port":  s.apiPort,
		"service":   "music-api",
	})
}

// sanitizeFileName limpia el nombre del archivo para evitar caracteres especiales
func sanitizeFileName(name string) string {
	reg := regexp.MustCompile(`[<>:"/\\|?*]`)
	safe := reg.ReplaceAllString(name, "_")
	if len(safe) > 100 {
		safe = safe[:100]
	}
	return strings.TrimSpace(safe)
}

func main() {
	// Leer configuración
	nodeID := getEnvAsInt("NODE_ID", 1)
	apiPort := 3003

	// Crear directorio de almacenamiento si no existe
	os.MkdirAll("storage/songs", 0755)

	server := NewServer(nodeID, apiPort)

	addr := fmt.Sprintf(":%d", apiPort)
	log.Printf("=== Music API Réplica %d iniciando ===", nodeID)
	log.Printf("API: http://0.0.0.0:%d", apiPort)
	log.Printf("Node ID: %d", nodeID)
	log.Printf("Storage: ./storage/songs/")
	log.Printf("===============================\n")

	if err := server.app.Listen(addr); err != nil {
		log.Fatalf("Error iniciando servidor: %v", err)
	}
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
