package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sort"
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
	Latency   time.Duration
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
	backendServiceName = "backend"       // Nombre exacto del servicio en docker-compose/stack
	backendPort        = 3003            // Puerto interno del contenedor (target port)
	discoveryInterval  = 5 * time.Second // Reducido de 15s a 5s para detectar cambios más rápido
)

func NewReverseProxy(serviceName string, port int) *ReverseProxy {
	// Crear service discovery
	rp := &ReverseProxy{
		backends:     make(map[int]*BackendNode),
		backendByURL: make(map[string]*BackendNode),
		healthCheck:  5 * time.Second, // Reducido de 10s a 5s
		client: &fasthttp.Client{
			NoDefaultUserAgentHeader: true,
			DisablePathNormalizing:   true,
			ReadTimeout:              60 * time.Second,
			WriteTimeout:             60 * time.Second,
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

	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Mapa temporal de URLs actuales descubiertas
	for _, ip := range addrs {
		url := fmt.Sprintf("http://%s:%d", ip, backendPort)

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
			IsAlive:   true, // Asumimos vivo al descubrirlo, el health check lo corregirá si no
			IsLeader:  false,
			ID:        newID,
			IP:        ip,
			LastCheck: time.Now(),
			Latency:   10 * time.Second,
		}

		rp.backends[newID] = newBackend
		rp.backendByURL[url] = newBackend

		log.Printf("[PROXY] Nueva réplica descubierta y añadida: ID %d → %s", newID, url)
	}
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
			alive, latency := rp.measureBackendHealth(b.URL)

			b.mu.Lock()
			b.IsAlive = alive
			b.Latency = latency
			if !alive {

				b.Latency = 5 * time.Second
			}
			b.LastCheck = time.Now()
			b.mu.Unlock()
		}(backend)
	}
	wg.Wait()
}

func (rp *ReverseProxy) measureBackendHealth(url string) (bool, time.Duration) {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url + "/health")
	req.Header.SetMethod("GET")

	start := time.Now()
	err := rp.client.DoTimeout(req, resp, 2*time.Second)
	latency := time.Since(start)

	alive := err == nil && resp.StatusCode() == fiber.StatusOK
	return alive, latency
}

type clusterResponse struct {
	NodeID   int  `json:"node_id"`
	IsLeader bool `json:"is_leader"`
}

func (rp *ReverseProxy) getClusterInfo(url string) (int, bool, time.Duration, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(url + "/cluster")
	req.Header.SetMethod("GET")

	start := time.Now()
	if err := rp.client.DoTimeout(req, resp, 2*time.Second); err != nil {
		return 0, false, time.Since(start), err
	}
	latency := time.Since(start)
	if resp.StatusCode() != 200 {
		return 0, false, latency, fmt.Errorf("status %d", resp.StatusCode())
	}

	var cr clusterResponse
	if err := json.Unmarshal(resp.Body(), &cr); err != nil {
		return 0, false, latency, err
	}

	return cr.NodeID, cr.IsLeader, latency, nil
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
		// Solo consultar si está vivo para no perder tiempo
		backend.mu.RLock()
		isAlive := backend.IsAlive
		backend.mu.RUnlock()

		if !isAlive {
			continue
		}

		nodeID, isLeader, latency, err := rp.getClusterInfo(backend.URL)
		if err != nil {
			continue
		}

		// Actualizar latencia
		backend.mu.Lock()
		backend.Latency = latency
		backend.ID = nodeID
		backend.IsLeader = isLeader
		backend.mu.Unlock()

		if isLeader {
			newLeaderID = nodeID
		}
	}

	if newLeaderID > 0 {
		rp.mu.Lock()
		if rp.leaderID != newLeaderID {
			log.Printf("[PROXY] Cambio de líder detectado: %d → %d", rp.leaderID, newLeaderID)
			rp.leaderID = newLeaderID
		}
		rp.mu.Unlock()
	}
}

func (rp *ReverseProxy) getLeaderBackend() (*BackendNode, bool) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	// Primero intentar buscar por el ID del líder que conocemos
	if rp.leaderID > 0 {
		// Buscar en el mapa de backends el nodo que tenga este ID
		for _, backend := range rp.backends {
			backend.mu.RLock()
			id := backend.ID
			isLeader := backend.IsLeader
			isAlive := backend.IsAlive
			backend.mu.RUnlock()

			if id == rp.leaderID && isAlive {
				// Doble verificación: ¿sigue diciendo que es líder?
				if isLeader {
					return backend, true
				}
			}
		}
	}

	// Si falla, buscar cualquiera que diga ser líder
	for _, backend := range rp.backends {
		backend.mu.RLock()
		isLeader := backend.IsLeader
		isAlive := backend.IsAlive
		backend.mu.RUnlock()

		if isAlive && isLeader {
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

func (rp *ReverseProxy) isWriteOperation(method, path string) bool {
	if method == "POST" || method == "PUT" || method == "DELETE" || method == "PATCH" {
		return true
	}
	return false
}

func (rp *ReverseProxy) CreateProxyHandler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Ensure basic CORS response headers in case middleware didn't handle preflight
		origin := string(c.Request().Header.Peek("Origin"))
		if origin != "" {
			c.Set("Access-Control-Allow-Origin", origin)
			c.Set("Access-Control-Allow-Credentials", "true")
			c.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With")
			c.Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS,PATCH")
		}

		// Handle preflight immediately
		if string(c.Request().Header.Method()) == "OPTIONS" {
			return c.SendStatus(fiber.StatusNoContent)
		}
		// Determinar a qué backend redirigir
		var targetBackend *BackendNode
		var found bool

		// Para operaciones de escritura, ir al líder
		method := c.Method()
		path := c.Path()

		// Definir qué rutas son de escritura
		isWriteOperation := rp.isWriteOperation(method, path)

		if isWriteOperation {
			// Intentar refrescar líder si no tenemos uno válido
			if rp.leaderID == 0 {
				rp.discoverLeader()
			}

			targetBackend, found = rp.getLeaderBackend()
			if !found {
				// Último intento: forzar descubrimiento rápido
				rp.discoverLeader()
				targetBackend, found = rp.getLeaderBackend()

				if !found {
					log.Printf("Lider No encontrado tras reintento")
					return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
						"error":  "No hay backends disponibles",
						"action": "try_again_later",
					})
				}
			}
		} else {
			// Para lecturas, usar cualquier backend vivo
			targetBackend, found = rp.getLowestLatencyBackend()
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

		if !strings.Contains(path, "auth") {
			path = "/api" + path
		}

		// Construir URL completa (enviar la ruta tal cual al backend)
		targetURL := targetBackend.URL + path
		if len(c.Request().URI().QueryString()) > 0 {
			targetURL += "?" + string(c.Request().URI().QueryString())
		}

		log.Printf("[PROXY] Target URL: %s", targetURL)

		req.SetRequestURI(targetURL)
		req.Header.SetMethod(method)

		// Copiar headers
		c.Request().Header.VisitAll(func(key, value []byte) {
			req.Header.SetBytesKV(key, value)
		})

		// Copiar body
		req.SetBody(c.Request().Body())

		// Ejecutar request
		if err := rp.client.DoTimeout(req, resp, 60*time.Second); err != nil {
			log.Printf("Error forwarding request: %v", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "Error connecting to backend",
			})
		}

		// Copiar respuesta
		c.Status(resp.StatusCode())
		resp.Header.VisitAll(func(key, value []byte) {
			c.Response().Header.SetBytesKV(key, value)
		})
		c.Response().SetBody(resp.Body())

		return nil
	}
}

func (rp *ReverseProxy) getLowestLatencyBackend() (*BackendNode, bool) {
	rp.mu.RLock()
	defer rp.mu.RUnlock()

	type candidate struct {
		node    *BackendNode
		latency time.Duration
	}

	var candidates []candidate

	for _, backend := range rp.backends {
		backend.mu.RLock()
		if backend.IsAlive {
			candidates = append(candidates, candidate{
				node:    backend,
				latency: backend.Latency,
			})
		}
		backend.mu.RUnlock()
	}

	if len(candidates) == 0 {
		return nil, false
	}

	// Ordenar por latencia ascendente
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].latency < candidates[j].latency
	})

	// Seleccionar el de menor latencia
	return candidates[0].node, true
}
