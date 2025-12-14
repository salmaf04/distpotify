package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"slices"
	"time"
)

func (s *Server) getAllNodeIDs() []int {
	const serviceName = "backend" // Cambia si tu servicio tiene otro nombre
	const clusterPath = "/cluster"
	const listenPort = 8080

	client := &http.Client{Timeout: 2 * time.Second}

	taskHost := "tasks." + serviceName
	addrs, err := net.LookupHost(taskHost)
	if err != nil || len(addrs) == 0 {
		log.Printf("Nodo %d no pudo resolver tareas de %s para obtener nodeIDs", s.nodeID, taskHost)
		return nil
	}

	var nodeIDs []int
	seen := make(map[int]bool)

	for _, ip := range addrs {
		// Evitar consultar a nosotros mismos innecesariamente
		url := fmt.Sprintf("http://%s:%d%s", ip, listenPort, clusterPath)

		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		var cr struct {
			NodeID int `json:"node_id"`
		}
		if json.NewDecoder(resp.Body).Decode(&cr) == nil {
			if cr.NodeID > 0 && !seen[cr.NodeID] {
				nodeIDs = append(nodeIDs, cr.NodeID)
				seen[cr.NodeID] = true
			}
		}
		resp.Body.Close()
	}

	// Opcional: ordenar para consistencia
	slices.Sort(nodeIDs)
	return nodeIDs
}

func (s *Server) getHigherNodes() []int {
	allNodes := s.getAllNodeIDs()
	if allNodes == nil {
		return nil
	}

	var higher []int
	for _, id := range allNodes {
		if id > s.nodeID {
			higher = append(higher, id)
		}
	}
	return higher
}
