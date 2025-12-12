package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"distributed-systems-project/handlers"
	"distributed-systems-project/models"
	"distributed-systems-project/structs"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Server struct {
	app         *fiber.App
	db          *gorm.DB
	sqlDB       *sql.DB
	nodeID      int
	apiPort     int
	isLeader    bool
	leaderID    int
	mu          sync.RWMutex
	songHandler *handlers.SongHandler
}

const (
	ElectionTimeout     = 3 * time.Second
	HeartbeatInterval   = 5 * time.Second
	CoordinationTimeout = 2 * time.Second
)

// === NUEVO: Mensajes del protocolo Bully ===
type ElectionMessage struct {
	Type   string `json:"type"` // "ELECTION", "ANSWER", "COORDINATOR"
	NodeID int    `json:"node_id"`
}

func (s *Server) nodeURL(nodeID int) string {
	port := 8080 + (nodeID - 1)
	return fmt.Sprintf("http://backend%d:%d", nodeID, port)
}

// Configuración de conexiones DB
func getDBConfig(nodeID int) string {
	// Cada réplica se conecta a su propia DB
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
	)
	if err != nil {
		log.Printf("Error en migración: %v", err)
	}

	// Crear handler de canciones
	songHandler := &handlers.SongHandler{DB: gormDB}

	// Crear app Fiber
	app := fiber.New(fiber.Config{
		AppName:       fmt.Sprintf("Music-Replica-%d", nodeID),
		CaseSensitive: true,
		StrictRouting: true,
	})

	// Middleware
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders: "Content-Type, Authorization, X-Data-Version, X-Node-ID",
	}))

	app.Use(logger.New(logger.Config{
		Format: "${time} [${ip}]:${port} ${status} - ${method} ${path}\n",
	}))

	server := &Server{
		app:         app,
		db:          gormDB,
		sqlDB:       sqlDB,
		nodeID:      nodeID,
		apiPort:     apiPort,
		isLeader:    false,
		leaderID:    0,
		songHandler: songHandler,
	}

	server.setupRoutes()
	go server.leaderElectionSimulation()
	go server.startHeartbeatMonitor()

	go server.initialSyncAfterJoin()

	return server
}

func (s *Server) setupRoutes() {
	// Health check para proxy
	s.app.Get("/health", s.healthHandler)
	s.app.Get("/cluster", s.clusterHandler)

	// API de canciones
	api := s.app.Group("/api")

	// Rutas de canciones con redirección automática
	api.Post("/songs/upload", s.uploadSongHandler) // Solo líder
	api.Post("/songs", s.createSongHandler)        // Solo líder
	api.Get("/songs", s.getSongsHandler)           // Cualquier réplica
	api.Get("/songs/:id", s.getSongByIDHandler)    // Cualquier réplica
	api.Get("/songs/search", s.searchSongsHandler) // Cualquier réplica
	api.Get("/songs/filter", s.filterSongsHandler) // Cualquier réplica

	s.app.Post("/internal/election", s.electionHandler)
	s.app.Get("/internal/songs/snapshot", s.songsSnapshotHandler)

	// Sincronización (para uso interno)
	s.app.Post("/internal/sync", s.syncHandler)

	// Forzar elección (testing)
	s.app.Post("/election/force", func(c *fiber.Ctx) error {
		if !s.isLeader {
			// Simular inicio de elección
			s.mu.Lock()
			s.leaderID = -1
			s.mu.Unlock()

			go s.leaderElectionSimulation()

			return c.JSON(fiber.Map{
				"message": "Elección forzada iniciada",
				"node_id": s.nodeID,
			})
		}
		return c.JSON(fiber.Map{
			"message": "Ya soy el líder",
			"node_id": s.nodeID,
		})
	})
}

func (s *Server) electionHandler(c *fiber.Ctx) error {
	var msg ElectionMessage
	if err := c.BodyParser(&msg); err != nil {
		return c.Status(400).SendString("bad request")
	}

	switch msg.Type {
	case "ELECTION":
		log.Printf("Nodo %d recibió ELECTION de nodo %d", s.nodeID, msg.NodeID)

		// Responder automáticamente si tengo mayor ID
		s.mu.RLock()
		myID := s.nodeID
		s.mu.RUnlock()

		if myID > msg.NodeID {
			// Responder ANSWER
			resp := ElectionMessage{Type: "ANSWER", NodeID: myID}
			c.JSON(resp)

			// Y si no estoy en elección, iniciar la mía propia
			go s.startLeaderElection()
		}

	case "COORDINATOR":
		log.Printf("Nodo %d recibe COORDINATOR de %d", s.nodeID, msg.NodeID)

		s.mu.Lock()
		if msg.NodeID > s.nodeID || s.leaderID < msg.NodeID {
			// Aceptar siempre si el nuevo líder tiene mayor ID
			s.leaderID = msg.NodeID
			s.isLeader = (msg.NodeID == s.nodeID)
			log.Printf("Nodo %d acepta a %d como líder", s.nodeID, msg.NodeID)
		}
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
			"redirect_to":  fmt.Sprintf("http://backend%d:%d/api/songs/upload", leaderID, 8080),
			"current_node": s.nodeID,
			"leader":       leaderID,
			"action":       "redirect_to_leader",
		})
	}

	// Soy el líder, procesar la subida
	return s.songHandler.UploadSong(c)
}

// Handler para creación de canción
func (s *Server) createSongHandler(c *fiber.Ctx) error {
	s.mu.RLock()
	isLeader := s.isLeader
	leaderID := s.leaderID
	s.mu.RUnlock()

	if !isLeader {
		// Redirigir al líder
		log.Printf("Soy nodo %d y estoy redirigiendo a nodo lider %v", s.nodeID, leaderID)

		return c.Status(fiber.StatusTemporaryRedirect).JSON(fiber.Map{
			"error":        "No soy el líder para operaciones de escritura",
			"redirect_to":  fmt.Sprintf("http://backend%d:%d/api/songs", leaderID, 8080),
			"current_node": s.nodeID,
			"leader":       leaderID,
			"action":       "redirect_to_leader",
		})
	}

	// Parsear entrada
	var songInput struct {
		Title    string  `json:"title"`
		Artist   string  `json:"artist"`
		Album    string  `json:"album"`
		Genre    string  `json:"genre"`
		Duration string  `json:"duration"`
		File     string  `json:"file"`
		Cover    *string `json:"cover,omitempty"`
	}

	if err := c.BodyParser(&songInput); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Crear estructura de entrada
	input := &structs.SongInputModel{
		Title:    songInput.Title,
		Artist:   songInput.Artist,
		Album:    songInput.Album,
		Genre:    songInput.Genre,
		Duration: songInput.Duration,
		File:     songInput.File,
		Cover:    songInput.Cover,
	}

	// Insertar canción
	result := s.songHandler.InsertSong(c, input)

	// Si se insertó correctamente, sincronizar con followers
	if c.Response().StatusCode() == fiber.StatusCreated {
		go s.syncNewSongToFollowers(input)
	}

	return result
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

// Health handler
func (s *Server) healthHandler(c *fiber.Ctx) error {
	// Verificar conexión a DB
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	dbOK := true
	if err := s.sqlDB.PingContext(ctx); err != nil {
		dbOK = false
	}

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

func (s *Server) syncNewSongToFollowers(song *structs.SongInputModel) {
	// Sincronizar nueva canción inmediatamente
	log.Printf("Sincronizando nueva canción con followers SIUUUU")

	var cover string
	if song.Cover != nil {
		cover = *song.Cover
	} else {
		cover = "" // o el valor por defecto que prefieras
	}

	// Convertir a modelo para sync
	newSong := models.Song{
		Title:    song.Title,
		Artist:   song.Artist,
		Album:    song.Album,
		Genre:    song.Genre,
		Duration: song.Duration,
		File:     song.File,
		Cover:    cover,
	}

	fmt.Printf("Estes es el id : %d", newSong.ID)

	// Enviar a followers
	for nodeID := 1; nodeID <= 3; nodeID++ {
		if nodeID != s.nodeID {
			go s.sendSongToNode(nodeID, newSong)
		}
	}
}

func main() {
	// Leer configuración
	nodeID := getEnvAsInt("NODE_ID", 1)
	apiPort := getEnvAsInt("API_PORT", 8080)

	// Ajustar puerto para evitar conflictos
	if nodeID > 1 {
		apiPort = apiPort + (nodeID - 1)
	}

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
