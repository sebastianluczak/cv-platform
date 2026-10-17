package main

type cleanupResult struct {
	deleted     int
	wouldDelete int
	failures    int
}

func (r *cleanupResult) add(other cleanupResult) {
	r.deleted += other.deleted
	r.wouldDelete += other.wouldDelete
	r.failures += other.failures
}
