package raft

type State int

const (
	Follower State = iota
	Candidate
	Leader
)
