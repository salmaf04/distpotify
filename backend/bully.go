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
	myIndex := s.lastAppliedIndex
	s.mu.Unlock()

	log.Printf("Nodo %d inicia elección (Bully Modificado - Index: %d)", myID, myIndex)

	allNodes := s.getAllNodeIDs()
	var targetNodes []int
	for _, id := range allNodes {
		if id != myID {
			targetNodes = append(targetNodes, id)
		}
	}

	if len(targetNodes) == 0 {
		s.declareVictory()
		return
	}

	answers := make(chan int, len(targetNodes))

	for _, nodeID := range targetNodes {
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
			log.Printf("Nodo %d recibió ANSWER de nodo %d (candidato mejor)", myID, responder)
			// Alguien con mejor criterio respondió -> abandono
			return

		case <-timer.C:
			// Timeout: nadie mejor respondió -> yo gano
			log.Printf("Nodo %d gana elección por timeout", myID)
			s.declareVictory()
			return
		}
	}
}

func (s *Server) sendElectionMessage(targetNodeID int) bool {
	s.mu.RLock()
	myIndex := s.lastAppliedIndex
	s.mu.RUnlock()

	msg := ElectionMessage{
		Type:      "ELECTION",
		NodeID:    s.nodeID,
		LastIndex: myIndex,
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
	allNodes := s.getAllNodeIDs()

	s.mu.RLock()
	myIndex := s.lastAppliedIndex
	s.mu.RUnlock()

	msg := ElectionMessage{
		Type:      "COORDINATOR",
		NodeID:    s.nodeID,
		LastIndex: myIndex,
	}
	jsonData, _ := json.Marshal(msg)

	for _, nodeID := range allNodes {
		if nodeID == s.nodeID {
			continue
		}
		go func(id int) {
			url := s.nodeURL(id) + "/internal/election"
			client := &http.Client{Timeout: CoordinationTimeout}
			client.Post(url, "application/json", bytes.NewBuffer(jsonData))
		}(nodeID)
	}

	log.Printf("Nodo %d envió COORDINATOR a %d nodos", s.nodeID, len(allNodes)-1)
}

func (s *Server) bootstrapLeadership() {
	time.Sleep(1 * time.Second) // dar tiempo a que otros arranquen
	leaderID := s.discoverLeaderByScanning()
	if leaderID > 0 {
		s.mu.Lock()
		s.leaderID = leaderID
		s.isLeader = (s.nodeID == leaderID)
		s.mu.Unlock()
		log.Printf("Nodo %d detecta líder %d al unirse", s.nodeID, leaderID)
		return
	}
	log.Printf("Nodo %d no encuentra líder, lanza elección Bully", s.nodeID)
	s.startLeaderElection()
}
