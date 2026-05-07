package traceroute

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"netprobe/internal/ws"

	gotraceroute "github.com/kjansson/go-traceroute"
)

// Hop represents a single router along the path to the target
type Hop struct {
	TTL     int      `json:"ttl"`
	IP      string   `json:"ip"`
	Host    string   `json:"host"`
	Latency float64  `json:"latency_ms"`
	Geo     *GeoInfo `json:"geo,omitempty"`
}

// GeoInfo holds geographic and network ownership data for an IP
type GeoInfo struct {
	City    string  `json:"city"`
	Country string  `json:"country"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	ISP     string  `json:"isp"`
	AS      string  `json:"as"`
}

// Result holds the complete traceroute with all hops
type Result struct {
	Target string `json:"target"`
	Hops   []Hop  `json:"hops"`
	Total  int    `json:"total_hops"`
}

// Run performs a UDP traceroute and streams each hop over WebSocket
func Run(target string, hub *ws.Hub) (*Result, error) {
	tracer := gotraceroute.New()
	tracer.Address = target
	tracer.MaxTTL = 30
	tracer.Timeout = 3 * time.Second
	tracer.DNSLookup = true

	traceResult, err := tracer.Trace()
	if err != nil {
		return nil, fmt.Errorf("traceroute failed: %w", err)
	}

	var hops []Hop
	for _, h := range traceResult.Hops {
		hop := Hop{
			TTL:     h.TTL,
			IP:      h.Address,
			Host:    h.Host,
			Latency: h.Latency,
		}

		// Look up geographic location for valid IPs
		if h.Address != "" && h.Address != "*" {
			geo, err := lookupGeo(h.Address)
			if err == nil {
				hop.Geo = geo
			}
		}

		hops = append(hops, hop)
		// Stream each hop to connected clients as it resolves
		hub.Broadcast(ws.Message{Type: "traceroute_hop", Target: target, Data: hop})
	}

	result := &Result{Target: target, Hops: hops, Total: len(hops)}
	hub.Broadcast(ws.Message{Type: "traceroute_complete", Target: target, Data: result})
	return result, nil
}

// lookupGeo calls ip-api.com to get lat/lon/city/country/ISP for an IP
func lookupGeo(ip string) (*GeoInfo, error) {
	resp, err := http.Get(fmt.Sprintf("http://ip-api.com/json/%s?fields=city,country,lat,lon,isp,as", ip))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		City    string  `json:"city"`
		Country string  `json:"country"`
		Lat     float64 `json:"lat"`
		Lon     float64 `json:"lon"`
		ISP     string  `json:"isp"`
		AS      string  `json:"as"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &GeoInfo{
		City: data.City, Country: data.Country,
		Lat: data.Lat, Lon: data.Lon,
		ISP: data.ISP, AS: data.AS,
	}, nil
}
