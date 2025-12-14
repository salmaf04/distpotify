package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
)

type BackendNode struct {
	URL       string
	IsLeader  bool
	IsAlive   bool
	ID        int
	IP        string
	LastCheck time.Time
	mu        sync.RWMutex
}

type ReverseProxy struct {
	backends     map[int]*BackendNode
	backendByURL map[string]*BackendNode
	leaderID     int
	mu           sync.RWMutex
	healthCheck  time.Duration
	client       *fasthttp.Client
	nextID       int
}

const (
	backendServiceName = "backend" // Nombre exacto del servicio en docker-compose/stack
	backendPort        = 3003      // Puerto interno del contenedor (target port)
	discoveryInterval  = 15 * time.Second
)

func NewReverseProxy(serviceName string, port int) *ReverseProxy {
	// Crear service discovery
	rp := &ReverseProxy{
		backends:     make(map[int]*BackendNode),
		backendByURL: make(map[string]*BackendNode),
		healthCheck:  10 * time.Second,
		client: &fasthttp.Client{
			NoDefaultUserAgentHeader: true,
			DisablePathNormalizing:   true,
			ReadTimeout:              30 * time.Second,
			WriteTimeout:             30 * time.Second,
			MaxConnsPerHost:          100,
		},
		nextID: 1,
	}

	rp.discoverBackends()

	// Health checks periódicos
	go rp.startHealthChecks()

	// Descubrimiento periódico (para nuevas réplicas)
	go func() {
		ticker := time.NewTicker(discoveryInterval)
		defer ticker.Stop()
		for range ticker.C {
			rp.discoverBackends()
			rp.discoverLeader() // también actualizamos líder
		}
	}()

	// Descubrir líder inicial
	go rp.discoverLeader()

	return rp
}

func (rp *ReverseProxy) discoverBackends() {
	taskHost := backendServiceName
	addrs, err := net.LookupHost(taskHost)
	if err != nil || len(addrs) == 0 {
		log.Printf("[PROXY] No se pudieron resolver tareas de %s: %v", taskHost, err)
		return
	}

	log.Printf("[PROXY] Descubiertas %d réplicas via DNS: %v", len(addrs), addrs)

	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Marcar todos como posiblemente obsoletos
	for _, b := range rp.backends {
		b.mu.Lock()
		b.IsAlive = false // los volveremos a validar en health check
		b.mu.Unlock()
	}

	// Mapa temporal de URLs actuales descubiertas
	currentURLs := make(map[string]bool)
	for _, ip := range addrs {
		url := fmt.Sprintf("http://%s:%d", ip, backendPort)
		currentURLs[url] = true

		// Si ya existe, continuar
		if existing, ok := rp.backendByURL[url]; ok {
			// Actualizar estado (se validará en health check)
			existing.mu.Lock()
			existing.LastCheck = time.Now()
			existing.mu.Unlock()
			continue
		}

		// Nuevo backend descubierto → añadirlo
		newID := rp.nextID
		rp.nextID++

		newBackend := &BackendNode{
			URL:       url,
			IsAlive:   false, // se comprobará en el próximo health check
			IsLeader:  false,
			ID:        newID,
			IP:        ip,
			LastCheck: time.Now(),
		}

		rp.backends[newID] = newBackend
		rp.backendByURL[url] = newBackend

		log.Printf("[PROXY] Nueva réplica descubierta y añadida: ID %d → %s", newID, url)
	}

	// Opcional: eliminar backends que ya no existen (después de un tiempo)
	// Por ahora los mantenemos pero marcados como !IsAlive
}

func (rp *ReverseProxy) startHealthChecks() {
	ticker := time.NewTicker(rp.healthCheck)
	defer ticker.Stop()

	for {
		<-ticker.C
		rp.checkAllBackends()
		rp.discoverLeader()
	}
}

func (rp *ReverseProxy) checkAllBackends() {
	var wg sync.WaitGroup

	log.Printf("Chequeando todos los backends")

	rp.mu.RLock()
	backends := make([]*BackendNode, 0, len(rp.backends))
	for _, backend := range rp.backends {
		backends = append(backends, backend)
	}
	rp.mu.RUnlock()

	for _, backend := range backends {
		wg.Add(1)
		go func(b *BackendNode) {
			defer wg.Done()
			alive := rp.isBackendAlive(b.URL)

			b.mu.Lock()
			b.IsAlive = alive
			b.LastCheck = time.Now()
			b.mu.Unlock()
		}(backend)
	}
	wg.Wait()
}

func (rp *ReverseProxy) isBackendAlive(url string) bool {
	log.Printf("Preguntando por los backends quien esta vivo")

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url + "/health")
	req.Header.SetMethod("GET")

	err := rp.client.DoTimeout(req, resp, 3*time.Second)
	if err != nil {
		log.Printf("Esto dio error %v", err)
		return false
	}

	log.Printf("Status Code: %d", resp.StatusCode())

	return resp.StatusCode() == fiber.StatusOK
}

type clusterResponse struct {
	NodeID   int  `json:"node_id"`
	IsLeader bool `json:"is_leader"`
}

func (rp *ReverseProxy) getClusterInfo(url string) (int, bool, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url + "/cluster")
	req.Header.SetMethod("GET")

	if err := rp.client.DoTimeout(req, resp, 3*time.Second); err != nil {
		return 0, false, err
	}

	if resp.StatusCode() != 200 {
		return 0, false, fmt.Errorf("status %d", resp.StatusCode())
	}

	var cr clusterResponse
	if err := json.Unmarshal(resp.Body(), &cr); err != nil {
		return 0, false, err
	}

	return cr.NodeID, cr.IsLeader, nil
}

func (rp *ReverseProxy) discoverLeader() {
	rp.mu.RLock()
	backends := make([]*BackendNode, 0, len(rp.backends))
	for _, b := range rp.backends {
		backends = append(backends, b)
	}
	rp.mu.RUnlock()

	var newLeaderID int = 0

	for _, backend := range backends {
		nodeID, isLeader, err := rp.getClusterInfo(backend.URL)
		if err != nil {
			continue
		}

		// Actualizar el ID real del backend (importante para consistencia)
		backend.mu.Lock()
		backend.ID = nodeID // ¡Ahora usamos el node_id real del backend!
		backend.mu.Unlock()

		if isLeader {
			newLeaderID = nodeID
			break
		}
	}

	if newLeaderID > 0 && newLeaderID != rp.leaderID {
		rp.mu.Lock()
		oldLeader := rp.leaderID
		rp.leaderID = newLeaderID

		// Actualizar IsLeader en todos
		for _, b := range rp.backends {
			b.mu.Lock()
			realID := b.ID
			b.IsLeader = (realID == newLeaderID)
			b.mu.Unlock()
		}
		rp.mu.Unlock()

		log.Printf("[PROXY] Cambio de líder detectado: %d → %d", oldLeader, newLeaderID)
	}
}

func (rp *ReverseProxy) checkIfLeader(url string) bool {
	log.Printf("Chequeando quien cojones es el lider , manda huevos 2:38 de la madrugada")

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url + "/cluster")
	req.Header.SetMethod("GET")

	err := rp.client.DoTimeout(req, resp, 3*time.Second)
	if err != nil {
		return false
	}

	// Parsear respuesta JSON

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return false
	}

	if isLeader, ok := result["is_leader"].(bool); ok {
		log.Printf("ya sabemos quien es el lider %s", url)
		return isLeader
	}
	return false
}

func (rp *ReverseProxy) getLeaderBackend() (*BackendNode, bool) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	log.Printf("Este es mi actual lider %d y lo tengo en status %v", rp.leaderID, rp.backends[rp.leaderID])

	if backend, exists := rp.backends[rp.leaderID]; exists {
		backend.mu.RLock()
		defer backend.mu.RUnlock()
		if backend.IsAlive && backend.IsLeader {
			return backend, true
		}
	}
	return nil, false
}

func (rp *ReverseProxy) getRandomBackend() (*BackendNode, bool) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	// Obtener lista de backends vivos
	var aliveBackends []*BackendNode
	for _, backend := range rp.backends {
		backend.mu.RLock()
		if backend.IsAlive {
			aliveBackends = append(aliveBackends, backend)
		}
		backend.mu.RUnlock()
	}

	if len(aliveBackends) == 0 {
		return nil, false
	}

	// Selección round-robin simple
	selected := aliveBackends[time.Now().Unix()%int64(len(aliveBackends))]
	return selected, true
}

func (rp *ReverseProxy) createProxyHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		log.Printf("Entrando al proxy - Request: %s %s", c.Method(), c.Path())

		// Determinar a qué backend redirigir
		var targetBackend *BackendNode
		var found bool

		// Para operaciones de escritura, ir al líder
		method := c.Method()
		path := c.Path()

		// Definir qué rutas son de escritura
		isWriteOperation := rp.isWriteOperation(method, path)

		if isWriteOperation {
			targetBackend, found = rp.getLeaderBackend()
			if !found {
				// Intentar encontrar cualquier backend vivo
				targetBackend, found = rp.getRandomBackend()
				if !found {
					log.Printf("Lider No encontrado")
					return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
						"error":  "No hay backends disponibles",
						"action": "try_again_later",
					})
				}
				// Log warning - escribiendo en no-líder
				log.Printf("[PROXY WARNING] Escritura en no-líder %d (no hay líder disponible)",
					targetBackend.ID)
			}

			log.Printf("Lider encontrado %d", targetBackend.ID)
		} else {
			// Para lecturas, usar cualquier backend vivo
			targetBackend, found = rp.getRandomBackend()
			if !found {
				return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
					"error": "No hay backends disponibles",
				})
			}
		}

		// Log de la redirección
		log.Printf("[PROXY] %s %s -> Backend %d (%s)",
			method, path, targetBackend.ID, targetBackend.URL)

		// Preparar la request para el backend
		req := fasthttp.AcquireRequest()
		defer fasthttp.ReleaseRequest(req)

		resp := fasthttp.AcquireResponse()
		defer fasthttp.ReleaseResponse(resp)

		// Construir URL completa
		targetURL := targetBackend.URL + path
		if len(c.Request().URI().QueryString()) > 0 {
			targetURL += "?" + string(c.Request().URI().QueryString())
		}

		req.SetRequestURI(targetURL)
		req.Header.SetMethod(method)

		// Copiar headers originales
		c.Request().Header.VisitAll(func(key, value []byte) {
			req.Header.SetBytesKV(key, value)
		})

		// Agregar headers de proxy
		req.Header.Set("X-Forwarded-For", c.IP())
		req.Header.Set("X-Proxy-Server", "music-reverse-proxy")
		req.Header.Set("X-Target-Backend", fmt.Sprintf("%d", targetBackend.ID))
		req.Header.Set("X-Original-Path", path)
		req.Header.Set("X-Node-ID", fmt.Sprintf("%d", targetBackend.ID))

		// Para operaciones de upload, manejar multipart/form-data
		if method == "POST" && strings.Contains(c.Get("Content-Type"), "multipart/form-data") {
			body := c.Body()
			req.SetBody(body)
			req.Header.SetContentType(c.Get("Content-Type"))
		} else if len(c.Body()) > 0 {
			req.SetBody(c.Body())
		}

		// Realizar la request al backend
		err := rp.client.Do(req, resp)
		if err != nil {
			log.Printf("[PROXY ERROR] Error al conectar con backend %d: %v", targetBackend.ID, err)

			// Marcar como no vivo
			targetBackend.mu.Lock()
			targetBackend.IsAlive = false
			targetBackend.mu.Unlock()

			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error":      "No se pudo conectar con el servidor backend",
				"backend_id": targetBackend.ID,
				"action":     "retry_with_other_node",
			})
		}

		// Copiar status code
		c.Status(resp.StatusCode())

		// Copiar headers de respuesta
		resp.Header.VisitAll(func(key, value []byte) {
			c.Response().Header.SetBytesKV(key, value)
		})

		// Asegurarse de que no se copien headers de transferencia
		c.Response().Header.Del("Transfer-Encoding")

		// Copiar body de respuesta
		c.Response().SetBody(resp.Body())

		return nil
	}
}

func (rp *ReverseProxy) isWriteOperation(method, path string) bool {
	if method == "POST" || method == "PUT" || method == "DELETE" || method == "PATCH" {
		return true
	}

	writePaths := []string{
		"/internal/sync",
		"/election/force",
	}

	for _, writePath := range writePaths {
		if strings.HasPrefix(path, writePath) {
			return true
		}
	}

	return false
}

func (rp *ReverseProxy) statusHandler(c *fiber.Ctx) error {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	status := make(map[string]interface{})
	backendsStatus := make(map[int]interface{})

	for id, backend := range rp.backends {
		backend.mu.RLock()
		backendsStatus[id] = fiber.Map{
			"url":            backend.URL,
			"ip":             backend.IP,
			"alive":          backend.IsAlive,
			"leader":         backend.IsLeader,
			"last_check":     backend.LastCheck.Format(time.RFC3339),
			"current_leader": rp.leaderID,
		}
		backend.mu.RUnlock()
	}

	status["backends"] = backendsStatus
	status["current_leader"] = rp.leaderID
	status["total_backends"] = len(rp.backends)
	status["timestamp"] = time.Now().Unix()

	return c.JSON(status)
}

func (rp *ReverseProxy) CreateProxyHandler() fiber.Handler {
	return rp.createProxyHandler() // o renombra directamente createProxyHandler a este nombre
}
