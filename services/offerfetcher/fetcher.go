package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/antchfx/htmlquery"
)

type ExternalFetcher struct {
	Name string
	URL  string
}

func NewExternalFetcher(name, url string) *ExternalFetcher {
	return &ExternalFetcher{
		Name: name,
		URL:  url,
	}
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
	}
}

func (ef *ExternalFetcher) Fetch(offersChan chan<- string, xpath string) {
	client := newHTTPClient()

	resp, err := client.Get(ef.URL)
	if err != nil {
		fmt.Printf("Error fetching offers from %s: %v\n", ef.Name, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Printf("Unexpected HTTP status from %s: %s\n", ef.Name, resp.Status)
		wholeBody, _ := io.ReadAll(resp.Body)
		fmt.Printf("Response body: %s\n", string(wholeBody))
		return
	}

	doc, err := htmlquery.Parse(resp.Body)
	if err != nil {
		fmt.Printf("Error parsing HTML from %s: %v\n", ef.Name, err)
		return
	}

	nodes, err := htmlquery.QueryAll(doc, xpath)
	if err != nil {
		fmt.Printf("Error querying offers from %s: %v\n", ef.Name, err)
		return
	}

	for _, node := range nodes {
		offersChan <- htmlquery.InnerText(node)
	}
}
