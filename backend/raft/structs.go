package raft

import (
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

type LogEntry struct {
	Term    int
	Command string
}

// Mensajes RAFT
type AppendEntriesReq struct {
	Term         int
	LeaderID     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesResp struct {
	Term    int
	Success bool
}

type RequestVoteReq struct {
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

type RequestVoteResp struct {
	Term        int
	VoteGranted bool
}

// Nodo RAFT
type RaftNode struct {
	mu sync.Mutex

	id          int
	peers       []int
	state       State
	currentTerm int
	votedFor    int
	log         []LogEntry

	commitIndex int
	lastApplied int

	electionTimeout  time.Duration
	heartbeatTimeout time.Duration

	votesReceived map[int]bool
	leaderID      int

	// Canales para comunicación
	appendEntriesChan chan AppendEntriesReq
	requestVoteChan   chan RequestVoteReq
}

func (myRaft *RaftNode) leaderCheckHandler(c *fiber.Ctx) error {
	if myRaft.state == Leader {
		c.Status(200)
		return c.SendString("ok")
	}
	return c.SendStatus(503) // o 404, da igual
}
