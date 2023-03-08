package event

import "math"

func IncSeqID(seq uint64) uint64 {
	if seq == math.MaxUint64 {
		seq = 0
	}
	seq++
	return seq
}
