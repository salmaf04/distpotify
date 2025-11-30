package raft

import (
	"fmt"
	"math/rand"
	"time"
)

func NewRaftNode(id int, peers []int) *RaftNode {
	return &RaftNode{
		id:                id,
		peers:             peers,
		state:             Follower,
		currentTerm:       0,
		votedFor:          -1,
		log:               make([]LogEntry, 0),
		commitIndex:       -1,
		lastApplied:       -1,
		electionTimeout:   time.Duration(150+rand.Intn(150)) * time.Millisecond,
		heartbeatTimeout:  50 * time.Millisecond,
		votesReceived:     make(map[int]bool),
		leaderID:          -1,
		appendEntriesChan: make(chan AppendEntriesReq, 100),
		requestVoteChan:   make(chan RequestVoteReq, 100),
	}
}

func (r *RaftNode) Run() {
	go r.startElectionTimer()
	go r.messageHandler()
	fmt.Printf("Nodo %d iniciado como Follower\n", r.id)
}

func (r *RaftNode) startElectionTimer() {
	for {
		timeout := r.electionTimeout
		time.Sleep(timeout)

		r.mu.Lock()
		if r.state != Leader {
			fmt.Printf("Nodo %d: timeout de elección, iniciando elección (término %d)\n", r.id, r.currentTerm+1)
			r.becomeCandidate()
		}
		r.mu.Unlock()
	}
}

func (r *RaftNode) becomeCandidate() {
	r.state = Candidate
	r.currentTerm++
	r.votedFor = r.id
	r.votesReceived = map[int]bool{r.id: true}

	lastLogIndex := len(r.log) - 1
	lastLogTerm := 0
	if lastLogIndex >= 0 {
		lastLogTerm = r.log[lastLogIndex].Term
	}

	for _, peer := range r.peers {
		go r.sendRequestVote(peer, RequestVoteReq{
			Term:         r.currentTerm,
			CandidateID:  r.id,
			LastLogIndex: lastLogIndex,
			LastLogTerm:  lastLogTerm,
		})
	}
}

func (r *RaftNode) becomeLeader() {
	r.state = Leader
	r.leaderID = r.id
	fmt.Printf("Nodo %d se convierte en LÍDER (término %d)\n", r.id, r.currentTerm)

	go r.startHeartbeat()
}

func (r *RaftNode) startHeartbeat() {
	for r.state == Leader {
		r.sendHeartbeats()
		time.Sleep(r.heartbeatTimeout)
	}
}

func (r *RaftNode) sendHeartbeats() {
	r.mu.Lock()
	if r.state != Leader {
		r.mu.Unlock()
		return
	}

	prevLogIndex := len(r.log) - 1
	prevLogTerm := 0
	if prevLogIndex >= 0 {
		prevLogTerm = r.log[prevLogIndex].Term
	}

	for _, peer := range r.peers {
		go r.sendAppendEntries(peer, AppendEntriesReq{
			Term:         r.currentTerm,
			LeaderID:     r.id,
			PrevLogIndex: prevLogIndex,
			PrevLogTerm:  prevLogTerm,
			Entries:      []LogEntry{},
			LeaderCommit: r.commitIndex,
		})
	}
	r.mu.Unlock()
}

func (r *RaftNode) sendAppendEntries(peer int, req AppendEntriesReq) {
	time.Sleep(10 * time.Millisecond)
	resp := r.handleAppendEntries(req)
	if resp.Term > r.currentTerm {
		r.mu.Lock()
		r.currentTerm = resp.Term
		r.state = Follower
		r.votedFor = -1
		r.mu.Unlock()
	}
}

func (r *RaftNode) sendRequestVote(peer int, req RequestVoteReq) {
	time.Sleep(10 * time.Millisecond)
	resp := r.handleRequestVote(req)
	if resp.VoteGranted {
		r.mu.Lock()
		r.votesReceived[peer] = true
		if r.state == Candidate && len(r.votesReceived) > len(r.peers)/2 {
			r.becomeLeader()
		}
		r.mu.Unlock()
	}
	if resp.Term > r.currentTerm {
		r.mu.Lock()
		r.currentTerm = resp.Term
		r.state = Follower
		r.votedFor = -1
		r.mu.Unlock()
	}
}

func (r *RaftNode) messageHandler() {
	for {
		select {
		case req := <-r.appendEntriesChan:
			r.handleAppendEntries(req)
		case req := <-r.requestVoteChan:
			r.handleRequestVote(req)
		}
	}
}

func (r *RaftNode) handleAppendEntries(req AppendEntriesReq) AppendEntriesResp {
	r.mu.Lock()
	defer r.mu.Unlock()

	if req.Term < r.currentTerm {
		return AppendEntriesResp{Term: r.currentTerm, Success: false}
	}

	go r.startElectionTimer()

	if req.Term > r.currentTerm {
		r.currentTerm = req.Term
		r.state = Follower
		r.votedFor = -1
	}

	r.leaderID = req.LeaderID

	if len(r.log) <= req.PrevLogIndex {
		return AppendEntriesResp{Term: r.currentTerm, Success: false}
	}
	if r.log[req.PrevLogIndex].Term != req.PrevLogTerm {
		return AppendEntriesResp{Term: r.currentTerm, Success: false}
	}

	r.log = append(r.log[:req.PrevLogIndex+1], req.Entries...)

	if req.LeaderCommit > r.commitIndex {
		r.commitIndex = min(req.LeaderCommit, len(r.log)-1)
		r.applyLog()
	}

	return AppendEntriesResp{Term: r.currentTerm, Success: true}
}

func (r *RaftNode) handleRequestVote(req RequestVoteReq) RequestVoteResp {
	r.mu.Lock()
	defer r.mu.Unlock()

	if req.Term < r.currentTerm {
		return RequestVoteResp{Term: r.currentTerm, VoteGranted: false}
	}

	if req.Term > r.currentTerm {
		r.currentTerm = req.Term
		r.state = Follower
		r.votedFor = -1
	}

	lastLogIndex := len(r.log) - 1
	lastLogTerm := 0
	if lastLogIndex >= 0 {
		lastLogTerm = r.log[lastLogIndex].Term
	}

	logOk := (req.LastLogTerm > lastLogTerm) ||
		(req.LastLogTerm == lastLogTerm && req.LastLogIndex >= lastLogIndex)

	if (r.votedFor == -1 || r.votedFor == req.CandidateID) && logOk {
		r.votedFor = req.CandidateID
		return RequestVoteResp{Term: r.currentTerm, VoteGranted: true}
	}

	return RequestVoteResp{Term: r.currentTerm, VoteGranted: false}
}

func (r *RaftNode) applyLog() {
	for r.lastApplied < r.commitIndex {
		r.lastApplied++
		entry := r.log[r.lastApplied]
		fmt.Printf("Nodo %d aplica: %s (término %d)\n", r.id, entry.Command, entry.Term)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	peers := []int{0, 1, 2}
	nodes := make([]*RaftNode, len(peers))

	for i, id := range peers {
		nodes[i] = NewRaftNode(id, peers)
	}

	for _, node := range nodes {
		node.Run()
	}

	time.Sleep(2 * time.Second)

	leader := nodes[1]
	leader.mu.Lock()
	if leader.state == Leader {
		leader.log = append(leader.log, LogEntry{Term: leader.currentTerm, Command: "SET x = 10"})
		fmt.Printf("Líder %d replicando comando\n", leader.id)
	}
	leader.mu.Unlock()

	select {}
}
