package config

// Stats returns the probability of any given operation being the one executed
// on any given step. You can multiply it by QPS to get the per operation QPS.
func Stats(scripts []*Script) map[string]float32 {
	opCounts := make(map[string]float32)
	var totalCount uint

	for _, script := range scripts {
		totalCount += script.Weight
		singleOpCount := float32(script.Weight) / float32(len(script.Steps))
		for _, step := range script.Steps {
			opCounts[step.Op] += singleOpCount
		}
	}

	opToQPS := make(map[string]float32, len(opCounts))
	for op, count := range opCounts {
		opToQPS[op] = count / float32(totalCount)
	}

	return opToQPS
}
