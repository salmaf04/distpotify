package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

func (s *Server) discoverLeaderByScanning() int {
	const serviceName = "backend"
	const clusterPath = "/cluster"
	const listenPort = 3003

	type clusterResp struct {
		NodeID   int  `json:"node_id"`
		IsLeader bool `json:"is_leader"`
	}

	client := &http.Client{Timeout: 2 * time.Second}

	// Resolver todas las IPs de las tareas/replicas usando el DNS interno de Swarm
	taskHost := serviceName
	addrs, err := net.LookupHost(taskHost)
	if err != nil {
		log.Printf("Nodo %d no pudo resolver DNS para %s: %v", s.nodeID, taskHost, err)
		return 0
	}

	if len(addrs) == 0 {
		log.Printf("Nodo %d no encontró tareas para %s", s.nodeID, taskHost)
		return 0
	}

	// Escanear cada IP encontrada (excluyendo la propia si es necesario)
	for _, ip := range addrs {
		url := fmt.Sprintf("http://%s:%d%s", ip, listenPort, clusterPath)

		resp, err := client.Get(url)
		if err != nil {
			continue // Timeout o conexión rechazada → siguiente
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var cr clusterResp
		if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		if cr.IsLeader {
			log.Printf("Nodo %d descubrió líder %d escaneando %s (%s)", s.nodeID, cr.NodeID, taskHost, ip)
			return cr.NodeID
		}
	}

	log.Printf("Nodo %d no encontró líder escaneando %s (%d tareas encontradas)", s.nodeID, taskHost, len(addrs))
	return 0
}
