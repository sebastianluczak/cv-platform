package main

import (
	"fmt"
	"sync"
)

func DoConcurrentWorkForLearning() {
	const workerCount = 3

	jobs := make(chan int)
	results := make(chan jobResult)

	// Start workers. Each worker receives jobs until the jobs channel is closed.
	var workers sync.WaitGroup
	for workerID := 1; workerID <= workerCount; workerID++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			for job := range jobs {
				results <- processJob(id, job)
			}
		}(workerID)
	}

	// The producer sends work, then closes jobs to tell workers there is no more.
	go func() {
		for _, job := range []int{2, 3, 4, 5, 6} {
			jobs <- job
		}
		close(jobs)
	}()

	// Close results only after every worker has finished sending to it.
	go func() {
		workers.Wait()
		close(results)
	}()

	// Collect results until all workers have finished.
	for result := range results {
		fmt.Printf("worker %d: %d squared is %d\n", result.workerID, result.job, result.square)
	}
}

type jobResult struct {
	workerID int
	job      int
	square   int
}

func processJob(workerID, job int) jobResult {
	return jobResult{
		workerID: workerID,
		job:      job,
		square:   job * job,
	}
}
