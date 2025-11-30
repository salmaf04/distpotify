package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
)

var cachedLeader atomic.Value // almacena string "10.0.0.42:8080" o ""
var lastChecked time.Time

func GetCurrentLeader(serviceName string, port int) (string, error) {
	if time.Since(lastChecked) < 4*time.Second {
		if leader := cachedLeader.Load(); leader != nil {
			return leader.(string), nil
		}
	}

	ips, err := net.LookupHost(serviceName)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no IPs resolved for %s", serviceName)
	}

	// Orden determinista = mejor comportamiento de cache
	sort.Strings(ips)

	// Timeout global para todo el discovery (nunca te cuelgues más de 800ms)
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	type result struct {
		leader string
		err    error
	}

	resultCh := make(chan result, 1)

	// Lanzamos una goroutine por IP
	for _, ip := range ips {
		go func(ip string) {
			url := fmt.Sprintf("http://%s:%d/_raft_leader", ip, port)

			// Importante: respetar el context
			req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
			resp, err := http.DefaultClient.Do(req)

			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				leader := net.JoinHostPort(ip, fmt.Sprintf("%d", port))
				// ¡Primer ganador gana! (no esperamos a los demás)
				select {
				case resultCh <- result{leader: leader}:
				default:
				}
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
			// Si falla, simplemente no enviamos nada
		}(ip)
	}

	// Esperamos al primer éxito o al timeout
	select {
	case r := <-resultCh:
		if r.err == nil && r.leader != "" {
			cachedLeader.Store(r.leader)
			lastChecked = time.Now()
			return r.leader, nil
		}
	case <-ctx.Done():
		// Timeout o cancelado
	}

	// Si llegamos aquí: nadie respondió 200 a tiempo
	cachedLeader.Store("")
	lastChecked = time.Now()
	return "", fmt.Errorf("no raft leader found (tried %d nodes)", len(ips))
}

func forwardToLeader(c *fiber.Ctx) error {
	leader, err := GetCurrentLeader("backend", 8080)
	if err != nil {
		return c.Status(503).SendString("No Raft leader available")
	}

	// Construimos una request estándar de net/http a partir de la de Fiber
	targetURL := fmt.Sprintf("http://%s%s", leader, c.OriginalURL())

	// Creamos la request estándar
	req, err := http.NewRequest(string(c.Method()), targetURL, bytes.NewReader(c.Request().Body()))
	if err != nil {
		return c.Status(500).SendString("Internal error")
	}

	// Copiamos todos los headers importantes
	c.Request().Header.VisitAll(func(key, value []byte) {
		req.Header.Set(string(key), string(value))
	})

	// Headers de proxy comunes
	if clientIP := c.Get("X-Forwarded-For"); clientIP == "" {
		req.Header.Set("X-Forwarded-For", c.IP())
	}
	req.Header.Set("X-Forwarded-Proto", c.Protocol())
	req.Header.Set("X-Forwarded-Host", c.Hostname())

	// Cliente HTTP reutilizable
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cachedLeader.Store("") // invalidamos cache
		return c.Status(502).SendString("Leader unreachable")
	}
	defer resp.Body.Close()

	// Copiamos headers de vuelta
	for k, vv := range resp.Header {
		for _, v := range vv {
			c.Set(k, v)
		}
	}

	// Evitamos que Fiber vuelva a escribir Content-Length
	c.Set("Connection", "close")

	c.Status(resp.StatusCode)

	if resp.ContentLength > 0 {
		return c.SendStream(resp.Body, int(resp.ContentLength))
	}
	return c.SendStream(resp.Body) // sin tamaño conocido
}
