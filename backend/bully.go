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

	// Verificar que el nodo está sincronizado antes de participar en elecciones
	s.syncMutex.RLock()
	isSynced := s.isSyncedWithLeader
	s.syncMutex.RUnlock()

	if !isSynced && s.leaderID > 0 {
		// Si hay un líder conocido pero no estamos sincronizados, no participar en elecciones
		log.Printf("Nodo %d no participa en elección (aún no sincronizado con líder %d)", s.nodeID, s.leaderID)
		s.mu.Unlock()
		return
	}

	myID := s.nodeID
	myIndex, myTimestamp := s.opLog.GetLastOperationInfo()
	s.mu.Unlock()

	log.Printf("Nodo %d inicia elección (Bully Mejorado - Index: %d, Timestamp: %d)",
		myID, myIndex, myTimestamp)

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
	index, timestamp := s.opLog.GetLastOperationInfo()

	msg := ElectionMessage{
		Type:          "ELECTION",
		NodeID:        s.nodeID,
		LastIndex:     index,
		LastTimestamp: timestamp,
	}

	jsonData, _ := json.Marshal(msg)
	url := s.nodeURL(targetNodeID) + "/internal/election"

	client := &http.Client{Timeout: CoordinationTimeout}
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))

	// Si hay error o no responde 200, asumimos nodo caído (no nos gana) -> return false
	if err != nil || resp.StatusCode != http.StatusOK {
		return false
	}
	defer resp.Body.Close()

	// CORRECCIÓN CRÍTICA: Leer la respuesta
	var responseMsg ElectionMessage
	if err := json.NewDecoder(resp.Body).Decode(&responseMsg); err != nil {
		// Si no podemos decodificar (ej: body vacío), asumimos que NO es un ANSWER
		return false
	}

	// Solo nos rendimos si el otro responde explícitamente "ANSWER"
	if responseMsg.Type == "ANSWER" {
		log.Printf("Nodo %d detiene elección: Nodo %d respondió ANSWER (Es mejor)", s.nodeID, targetNodeID)
		return true
	}

	return false
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

	index, timestamp := s.opLog.GetLastOperationInfo()

	msg := ElectionMessage{
		Type:          "COORDINATOR",
		NodeID:        s.nodeID,
		LastIndex:     index,
		LastTimestamp: timestamp,
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

	// Al arrancar o recuperarse, el nodo no está sincronizado
	s.syncMutex.Lock()
	s.isSyncedWithLeader = false
	s.syncMutex.Unlock()

	leaderID := s.discoverLeaderByScanning()
	if leaderID > 0 {
		s.mu.Lock()
		s.leaderID = leaderID
		s.isLeader = (s.nodeID == leaderID)
		s.mu.Unlock()

		log.Printf("Nodo %d detecta líder %d al unirse (iniciando sincronización)", s.nodeID, leaderID)

		// Forzar sincronización inmediata con el líder
		s.syncDataFromLeader()

		// Marcar como sincronizado después de la sincronización
		s.syncMutex.Lock()
		s.isSyncedWithLeader = true
		s.syncMutex.Unlock()

		return
	}
	log.Printf("Nodo %d no encuentra líder, lanza elección Bully", s.nodeID)

	// Si es el único nodo, marcarse como sincronizado
	s.syncMutex.Lock()
	s.isSyncedWithLeader = true
	s.syncMutex.Unlock()

	s.startLeaderElection()
}
