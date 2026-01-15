package main

import (
	"distributed-systems-project/models"
	"encoding/json"
	"sync"
	"time"

	"gorm.io/gorm"
)

type OperationType string

const (
	OpCreate        OperationType = "CREATE"
	OpCreateUser    OperationType = "CREATE_USER"
	OpUpdate        OperationType = "UPDATE"
	OpDelete        OperationType = "DELETE"
	OpCreateSession OperationType = "CREATE_SESSION"
	OnUpdateUser    OperationType = "UPDATE_USER"
	OnUpdateSession OperationType = "UPDATE_SESSION"
)

type Operation struct {
	Index       int64           `json:"index"`
	Type        OperationType   `json:"type"`
	Data        models.Song     `json:"data"`
	UserData    *models.User    `json:"user_data,omitempty"`    // Nuevo campo
	SessionData *models.Session `json:"session_data,omitempty"` // Nuevo campo
	Timestamp   int64           `json:"timestamp"`
}

type OpLog struct {
	mu sync.RWMutex
	db *gorm.DB
}

func NewOpLog(db *gorm.DB) *OpLog {
	return &OpLog{
		db: db,
	}
}

func (l *OpLog) AppendSession(opType OperationType, data models.Session) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	var lastOp models.OperationLog
	var newIndex int64 = 1
	if err := l.db.Order("index desc").First(&lastOp).Error; err == nil {
		newIndex = lastOp.Index + 1
	}

	jsonData, _ := json.Marshal(data)

	opLog := models.OperationLog{
		ID:          uint(newIndex),
		Index:       newIndex,
		Type:        string(opType),
		SessionData: jsonData, // Asegúrate de tener este campo en el modelo DB o usar Data genérico
		Timestamp:   time.Now().UnixNano(),
	}

	opLog.ID = uint(newIndex)
	l.db.Create(&opLog)
	return newIndex
}

func (l *OpLog) Append(opType OperationType, data models.Song) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	var lastOp models.OperationLog
	var newIndex int64 = 1

	if err := l.db.Order("index desc").First(&lastOp).Error; err == nil {
		newIndex = lastOp.Index + 1
	}

	jsonData, _ := json.Marshal(data)

	opLog := models.OperationLog{
		ID:        uint(newIndex),
		Index:     newIndex,
		Type:      string(opType),
		Data:      jsonData,
		Timestamp: time.Now().UnixNano(),
	}

	opLog.ID = uint(newIndex)

	l.db.Create(&opLog)
	return newIndex
}

func (l *OpLog) AppendUser(opType OperationType, data models.User) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	var lastOp models.OperationLog
	var newIndex int64 = 1
	if err := l.db.Order("index desc").First(&lastOp).Error; err == nil {
		newIndex = lastOp.Index + 1
	}

	jsonData, _ := json.Marshal(data)

	opLog := models.OperationLog{
		ID:        uint(newIndex),
		Index:     newIndex,
		Type:      string(opType),
		UserData:  jsonData,
		Timestamp: time.Now().UnixNano(),
	}

	l.db.Create(&opLog)
	return newIndex
}

func (l *OpLog) GetSince(index int64) ([]Operation, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var logs []models.OperationLog
	// Buscar operaciones posteriores al índice dado
	if err := l.db.Where("index > ?", index).Order("index asc").Find(&logs).Error; err != nil {
		return nil, false
	}

	// Verificar si hay huecos (si el nodo está pidiendo algo más viejo de lo que tenemos)
	var firstOp models.OperationLog
	if err := l.db.Order("index asc").First(&firstOp).Error; err == nil {
		if index < firstOp.Index-1 {
			return nil, false // Se requiere snapshot completo
		}
	}

	// Convertir de modelo DB a estructura API
	ops := make([]Operation, len(logs))
	for i, log := range logs {
		var song models.Song
		var user *models.User
		var session *models.Session

		if len(log.Data) > 0 {
			json.Unmarshal(log.Data, &song)
		}
		if len(log.UserData) > 0 {
			u := models.User{}
			json.Unmarshal(log.UserData, &u)
			user = &u
		}
		if len(log.SessionData) > 0 { // Asumiendo columna nueva
			s := models.Session{}
			json.Unmarshal(log.SessionData, &s)
			session = &s
		}

		ops[i] = Operation{
			Index:       log.Index,
			Type:        OperationType(log.Type),
			Data:        song,
			UserData:    user,
			SessionData: session,
			Timestamp:   log.Timestamp,
		}
	}

	return ops, true
}

// GetLastOperationInfo retorna el índice y timestamp de la última operación
// para usarse en comparaciones de elección de líder
func (l *OpLog) GetLastOperationInfo() (index int64, timestamp int64) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var lastOp models.OperationLog
	if err := l.db.Order("index desc").First(&lastOp).Error; err != nil {
		return 0, 0
	}
	return lastOp.Index, lastOp.Timestamp
}
