package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

func (s *Server) discoverLeaderByScanning() int {
	basePort := 8080

	type clusterResp struct {
		NodeID   int  `json:"node_id"`
		IsLeader bool `json:"is_leader"`
	}

	client := &http.Client{Timeout: 2 * time.Second}

	for i := 1; i <= 6; i++ {
		if i == s.nodeID {
			continue
		}

		port := basePort
		if i > 1 {
			port = basePort + (i - 1)
		}

		url := fmt.Sprintf("http://backend%d:%d/cluster", i, port)

		resp, err := client.Get(url)
		if err != nil {
			continue
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
			log.Printf("Nodo %d descubrió líder %d escaneando backend%d", s.nodeID, cr.NodeID, i)
			return cr.NodeID
		}
	}

	log.Printf("Nodo %d no encontró líder escaneando backend1..backend6", s.nodeID)
	return 0
}
