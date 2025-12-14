package main

import (
	"distributed-systems-project/models"
	"sync"
	"time"
)

type OperationType string

const (
	OpCreate OperationType = "CREATE"
	OpUpdate OperationType = "UPDATE"
	OpDelete OperationType = "DELETE"
)

type Operation struct {
	Index     int64         `json:"index"`
	Type      OperationType `json:"type"`
	Data      models.Song   `json:"data"`
	Timestamp int64         `json:"timestamp"`
}

type OpLog struct {
	mu         sync.RWMutex
	operations []Operation
	maxSize    int
	startIndex int64 // Índice del primer elemento en el buffer
}

func NewOpLog(maxSize int) *OpLog {
	return &OpLog{
		operations: make([]Operation, 0, maxSize),
		maxSize:    maxSize,
		startIndex: 1,
	}
}

func (l *OpLog) Append(opType OperationType, data models.Song) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	lastIndex := l.startIndex + int64(len(l.operations)) - 1
	if len(l.operations) == 0 {
		lastIndex = l.startIndex - 1
	}

	newIndex := lastIndex + 1

	op := Operation{
		Index:     newIndex,
		Type:      opType,
		Data:      data,
		Timestamp: time.Now().UnixNano(),
	}

	if len(l.operations) >= l.maxSize {
		// Buffer lleno: eliminar el más antiguo
		l.operations = l.operations[1:]
		l.startIndex++
	}

	l.operations = append(l.operations, op)
	return newIndex
}

// GetSince devuelve operaciones estrictamente DESPUÉS del índice dado
func (l *OpLog) GetSince(index int64) ([]Operation, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if len(l.operations) == 0 {
		if index == 0 {
			return []Operation{}, true
		}
		return nil, false // Log vacío pero piden > 0
	}

	firstLogIndex := l.startIndex
	lastLogIndex := l.startIndex + int64(len(l.operations)) - 1

	// Si piden algo más viejo de lo que tenemos, necesitamos Snapshot
	if index < firstLogIndex-1 {
		return nil, false
	}

	if index >= lastLogIndex {
		return []Operation{}, true
	}

	offset := index - firstLogIndex + 1
	if offset < 0 {
		offset = 0
	}

	// Copiar para evitar condiciones de carrera
	result := make([]Operation, len(l.operations[offset:]))
	copy(result, l.operations[offset:])

	return result, true
}
