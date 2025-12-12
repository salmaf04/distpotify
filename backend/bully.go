package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func (s *Server) startLeaderElection() {
	s.mu.Lock()
	if s.isLeader {
		s.mu.Unlock()
		return
	}
	myID := s.nodeID
	s.mu.Unlock()

	log.Printf("Nodo %d inicia elección (Bully Algorithm)", myID)

	// Enviar mensaje ELECTION a todos los nodos con ID mayor
	higherNodes := s.getHigherNodes()
	if len(higherNodes) == 0 {
		// No hay nadie con mayor ID → yo soy el líder
		s.declareVictory()
		return
	}

	answers := make(chan int, len(higherNodes))
	timeouts := 0

	for _, nodeID := range higherNodes {
		go func(id int) {
			if s.sendElectionMessage(id) {
				answers <- id
			}
		}(nodeID)
	}

	// Esperar respuestas
	timer := time.NewTimer(ElectionTimeout)
	defer timer.Stop()

	for {
		select {
		case responder := <-answers:
			log.Printf("Nodo %d recibió ANSWER de nodo %d", myID, responder)
			// Alguien mayor está vivo → abandono la elección
			return

		case <-timer.C:
			// Timeout: nadie respondió → yo gano
			if timeouts == 0 { // primera vez que se acaba el tiempo
				log.Printf("Nodo %d no recibió respuestas → se declara ganador", myID)
				s.declareVictory()
			}
			return
		}
	}
}

func (s *Server) sendElectionMessage(targetNodeID int) bool {
	msg := ElectionMessage{
		Type:   "ELECTION",
		NodeID: s.nodeID,
	}

	jsonData, _ := json.Marshal(msg)
	url := s.nodeURL(targetNodeID) + "/internal/election"

	client := &http.Client{Timeout: CoordinationTimeout}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))

	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Nodo %d no responde a ELECTION (probablemente caído)", targetNodeID)
		return false
	}
	defer resp.Body.Close()
	return true
}

func (s *Server) declareVictory() {
	s.mu.Lock()
	s.isLeader = true
	s.leaderID = s.nodeID
	s.mu.Unlock()

	log.Printf("Nodo %d SE DECLARA LÍDER OFICIAL", s.nodeID)

	// Anunciar a todos los nodos menores que yo soy el nuevo coordinador
	s.broadcastCoordinator()
}

func (s *Server) broadcastCoordinator() {
	msg := ElectionMessage{
		Type:   "COORDINATOR",
		NodeID: s.nodeID,
	}

	jsonData, _ := json.Marshal(msg)

	for nodeID := 1; nodeID <= 3; nodeID++ {
		if nodeID == s.nodeID {
			continue
		}
		go func(id int) {
			url := s.nodeURL(id) + "/internal/election"
			client := &http.Client{Timeout: CoordinationTimeout}
			client.Post(url, "application/json", bytes.NewBuffer(jsonData))
			// No nos importa si falla, eventualmente se enterará por heartbeat o sync
		}(nodeID)
	}
}

func (s *Server) leaderElectionSimulation() {
	time.Sleep(5 * time.Second)

	// En una implementación real, usarías el algoritmo Bully
	// Aquí simulamos que el nodo 1 es el líder
	if s.nodeID == 1 {
		s.mu.Lock()
		s.isLeader = true
		s.leaderID = s.nodeID
		s.mu.Unlock()
		log.Printf("Nodo %d se declara líder", s.nodeID)
	} else {
		s.mu.Lock()
		s.leaderID = 1
		s.mu.Unlock()
	}

	// Heartbeat para líder
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			s.mu.RLock()
			isLeader := s.isLeader
			s.mu.RUnlock()

			if isLeader {
				log.Printf("Líder %d activo - Heartbeat", s.nodeID)
			}
		}
	}()
}
