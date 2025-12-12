package main

func (s *Server) getHigherNodes() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var higher []int
	for i := s.nodeID + 1; i <= 3; i++ { // asumiendo máximo 3 nodos
		higher = append(higher, i)
	}
	return higher
}
