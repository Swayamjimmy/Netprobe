package dns

import (
	"fmt"
	"sort"
	"time"

	"netprobe/internal/ws"

	mdns "github.com/miekg/dns"
)

// ResolverResult holds benchmark data for a single DNS resolver
type ResolverResult struct {
	Name    string   `json:"name"`
	Address string   `json:"address"`
	AvgMs   float64  `json:"avg_ms"`
	P95Ms   float64  `json:"p95_ms"`
	Answers []string `json:"answers"`
}

// Result holds the comparison of all tested resolvers
type Result struct {
	Target    string           `json:"target"`
	Resolvers []ResolverResult `json:"resolvers"`
	Fastest   string           `json:"fastest"`
}

// resolvers to benchmark against
var resolvers = []struct {
	Name    string
	Address string
}{
	{"Google", "8.8.8.8:53"},
	{"Cloudflare", "1.1.1.1:53"},
	{"Quad9", "9.9.9.9:53"},
	{"OpenDNS", "208.67.222.222:53"},
}

// Benchmark queries the target domain against all resolvers and ranks them
func Benchmark(target string, hub *ws.Hub) (*Result, error) {
	var results []ResolverResult

	for _, r := range resolvers {
		rr, err := benchmarkResolver(target, r.Name, r.Address)
		if err != nil {
			continue
		}
		results = append(results, *rr)
		// Stream each resolver result as it completes
		hub.Broadcast(ws.Message{Type: "dns_result", Target: target, Data: rr})
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("all resolvers failed")
	}

	// Sort by average response time to find the fastest
	sort.Slice(results, func(i, j int) bool {
		return results[i].AvgMs < results[j].AvgMs
	})

	result := &Result{
		Target:    target,
		Resolvers: results,
		Fastest:   results[0].Name,
	}

	hub.Broadcast(ws.Message{Type: "dns_complete", Target: target, Data: result})
	return result, nil
}

// benchmarkResolver runs 5 queries against a single resolver and computes avg/p95
func benchmarkResolver(target, name, address string) (*ResolverResult, error) {
	client := &mdns.Client{Timeout: 5 * time.Second}
	msg := &mdns.Msg{}
	msg.SetQuestion(mdns.Fqdn(target), mdns.TypeA)

	var times []float64
	var answers []string

	// Run 5 queries to get a stable average
	for i := 0; i < 5; i++ {
		start := time.Now()
		resp, _, err := client.Exchange(msg, address)
		elapsed := time.Since(start)

		if err != nil {
			continue
		}

		times = append(times, float64(elapsed.Microseconds())/1000.0)

		// Capture the resolved IPs from the first successful query
		if i == 0 && resp != nil {
			for _, a := range resp.Answer {
				if aRecord, ok := a.(*mdns.A); ok {
					answers = append(answers, aRecord.A.String())
				}
			}
		}
	}

	if len(times) == 0 {
		return nil, fmt.Errorf("resolver %s failed all queries", name)
	}

	// Calculate average and p95 latency
	sort.Float64s(times)
	avg := 0.0
	for _, t := range times {
		avg += t
	}
	avg /= float64(len(times))

	p95Idx := int(float64(len(times)) * 0.95)
	if p95Idx >= len(times) {
		p95Idx = len(times) - 1
	}

	return &ResolverResult{
		Name:    name,
		Address: address,
		AvgMs:   avg,
		P95Ms:   times[p95Idx],
		Answers: answers,
	}, nil
}
