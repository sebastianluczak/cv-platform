package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {
	fmt.Println("Offerfetcher service started")

	fetcher := NewExternalFetcher("PHPOfferFetcher", "https://nofluffjobs.com/pl/PHP")

	var wg sync.WaitGroup
	offersChan := make(chan string)
	// wg.Go(func() {
	// 	fetcher.Fetch()
	// 	time.Sleep(time.Second * 10)
	// })

	// can we do recursive fetching?
	// we could potentially call fetcher.Fetch() again here or set up a loop with a ticker for periodic fetching
	// for example, using a ticker:
	ticker := time.NewTicker(time.Second * 2)
	wg.Go(func() {
		for range ticker.C {
			fetcher.Fetch(offersChan, "//h3")
		}
	})

	wg.Go(func() {
		wg.Wait()
		close(offersChan)
	})

	wg.Go(func() {
		database := &PostgresDatabase{ConnStr: "postgres://cv_platform:cv_platform@localhost:5432/cv_platform?sslmode=disable"}
		db, err := database.Connect()
		if err != nil {
			fmt.Println("failed to connect to database:", err)
			return
		}
		for offer := range offersChan {
			fmt.Println("offer:", offer)
			fmt.Println("received offer from channel")
			// Insert the offer into the database
			err := database.InsertOffer(db, OfferRecord{Title: offer, Link: ""})
			if err != nil {
				fmt.Println("failed to insert offer into database:", err)
			}
		}
	})

	wg.Wait()

	// alternatively, we could implement a recursive fetching mechanism here if needed
	// Example of a simple recursive fetching mechanism:
	// go func() {
	//     for {
	//         fetcher.Fetch()
	//         time.Sleep(time.Minute * 10)
	//     }
	// }()

	// Or, we can use func() returning function for recursive fetching
	// Example:
	// func recursiveFetch(fetcher *ExternalFetcher) func() {
	//     return func() {
	//         fetcher.Fetch()
	//         time.Sleep(time.Minute * 10)
	//         recursiveFetch(fetcher)()
	//     }
	// }
	// go recursiveFetch(fetcher)()

	// I think all are valid.
}
