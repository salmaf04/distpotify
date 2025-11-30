package raft

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
)

var (
	once     sync.Once
	instance *RaftNode
)

// GetNode devuelve la única instancia del nodo Raft (singleton seguro para concurrencia)
func GetNode() *RaftNode {
	once.Do(func() {
		instance = bootstrapNode()
		go instance.Run() // arranca el state machine en background
	})
	return instance
}

// bootstrapNode construye el nodo a partir de variables de entorno
func bootstrapNode() *RaftNode {
	myID := mustEnvInt("NODE_ID")
	peersStr := os.Getenv("PEERS") // ej: "1,2,3"

	peers := make([]int, 0)
	if peersStr != "" {
		for _, s := range strings.Split(peersStr, ",") {
			if s = strings.TrimSpace(s); s != "" {
				id, _ := strconv.Atoi(s)
				peers = append(peers, id)
			}
		}
	}

	node := NewRaftNode(myID, peers)
	return node
}

// helper
func mustEnvInt(key string) int {
	val := os.Getenv(key)
	if val == "" {
		panic("Variable de entorno requerida no encontrada: " + key)
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		panic("NODE_ID debe ser un número entero")
	}
	return i
}

func LeaderCheckHandler(c *fiber.Ctx) error {
	return GetNode().leaderCheckHandler(c) // aquí sí puedes llamar al privado
}
