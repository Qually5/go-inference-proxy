package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// Target represents a backend AI model server
type Target struct {
	URL    *url.URL
	Active bool
	Proxy  *httputil.ReverseProxy
	mu     sync.RWMutex // Mutex for active status
}

// SetActive sets the active status of the target
func (t *Target) SetActive(active bool) {
	t.mu.Lock()
	t.Active = active
	t.mu.Unlock()
}

// IsActive returns the active status of the target
func (t *Target) IsActive() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Active
}

// LoadBalancer manages multiple AI model targets and their health
type LoadBalancer struct {
	Targets []*Target
	Count   int
	mu      sync.RWMutex // Mutex for targets and count
}

// AddTarget adds a new target to the load balancer
func (lb *LoadBalancer) AddTarget(targetURL string) error {
	url, err := url.Parse(targetURL)
	if err != nil {
		return err
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.Targets = append(lb.Targets, &Target{URL: url, Active: true, Proxy: httputil.NewSingleHostReverseProxy(url)})
	log.Printf("Added new target: %s", targetURL)
	return nil
}

// getNextPeer returns the next active target using a round-robin approach
func (lb *LoadBalancer) getNextPeer() *Target {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	activeTargets := []*Target{}
	for _, t := range lb.Targets {
		if t.IsActive() {
			activeTargets = append(activeTargets, t)
		}
	}

	if len(activeTargets) == 0 {
		return nil
	}

	// Simple round-robin
	peer := activeTargets[lb.Count%len(activeTargets)]
	lb.Count++ // This should ideally be protected by a mutex if multiple goroutines access it
	return peer
}

// healthCheck periodically checks the health of each target
func (lb *LoadBalancer) healthCheck() {
	for {
		lb.mu.RLock()
		for _, t := range lb.Targets {
			go func(target *Target) {
				log.Printf("Checking health of %s", target.URL.String())
				resp, err := http.Get(target.URL.String() + "/health") // Assuming a /health endpoint
				if err != nil || resp.StatusCode != http.StatusOK {
					if target.IsActive() {
						target.SetActive(false)
						log.Printf("Target %s is down", target.URL.String())
					}
				} else {
					if !target.IsActive() {
						target.SetActive(true)
						log.Printf("Target %s is back up", target.URL.String())
					}
				}
			}(t)
		}
		lb.mu.RUnlock()
		time.Sleep(10 * time.Second)
	}
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	peer := lb.getNextPeer()
	if peer == nil {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	log.Printf("Proxying request to: %s
", peer.URL)
	peer.Proxy.ServeHTTP(w, r)
}

func main() {
	lb := &LoadBalancer{}

	// Initial backend AI model endpoints
	initialTargets := []string{
		"http://localhost:8081",
		"http://localhost:8082",
		"http://localhost:8083",
	}

	for _, t := range initialTargets {
		if err := lb.AddTarget(t); err != nil {
			log.Fatalf("Failed to add target %s: %v", t, err)
		}
	}

	// Start health checking in a goroutine
	go lb.healthCheck()

	server := http.Server{
		Addr:    ":8080",
		Handler: lb,
	}

	fmt.Println("Starting Go Inference Proxy on port 8080...")
	log.Fatal(server.ListenAndServe())
}
