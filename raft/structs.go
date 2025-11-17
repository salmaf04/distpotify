package raft

import (
	"sync"
	"time"
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
