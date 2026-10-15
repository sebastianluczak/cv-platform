package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type requestResult struct {
	ID        int
	Uri       string
	Result    int
	Exception error
}

func ConcurrentRequests() {
	baseUrl := "https://developer.sabre.com/api/v1/products/"
	products := []string{
		"sdk/agency-workspace/26.9/help-documentation/sdk-overview.html",
		"sdk/agency-workspace/26.9/help-documentation/legal-and-advisories.html",
		"sdk/agency-workspace/26.9/help-documentation/web-modules-overview.html",
		"sdk/agency-workspace/26.9/help-documentation/web-modules-structure.html",
		"sdk/agency-workspace/26.9/help-documentation/web-red-app-creating.html",
		"sdk/agency-workspace/26.9/help-documentation/web-red-app-building.html",
	}

	// Create workers first
	const workerCount = 2
	var workers sync.WaitGroup

	jobs := make(chan string)
	results := make(chan requestResult)

	for WorkerId := 1; WorkerId <= workerCount; WorkerId++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			for job := range jobs {
				res, err := processHealthCheckForUri(id, job)
				if err != nil {
					results <- requestResult{
						ID:        id,
						Uri:       job,
						Exception: err,
					}
					continue
				}
				results <- res
			}
		}(WorkerId)
	}

	// Now do the work
	go func() {
		defer close(jobs)

		for _, product := range products {
			jobs <- baseUrl + product
		}
	}()
	// Close results only after every worker has finished sending to it.
	go func() {
		workers.Wait()
		close(results)
	}()

	// Collect results until all workers have finished.
	for result := range results {
		fmt.Printf("worker %d: %s returns status code %d\n",
			result.ID, result.Uri, result.Result)
	}
}

func processHealthCheckForUri(id int, urlToCheck string) (requestResult, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	result, err := client.Get(urlToCheck)
	if err != nil {
		return requestResult{ID: id, Uri: urlToCheck, Result: 500},
			fmt.Errorf("GET %s: %w", urlToCheck, err)
	}
	defer result.Body.Close()

	return requestResult{
		ID:     id,
		Uri:    urlToCheck,
		Result: result.StatusCode,
	}, nil
}
