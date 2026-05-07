package traceroute

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"netprobe/internal/ws"
)

type Hop struct {
	TTL     int      `json:"ttl"`
	IP      string   `json:"ip"`
	Host    string   `json:"host"`
	Latency float64  `json:"latency_ms"`
	Geo     *GeoInfo `json:"geo,omitempty"`
}

type GeoInfo struct {
	City    string  `json:"city"`
	Country string  `json:"country"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	ISP     string  `json:"isp"`
	AS      string  `json:"as"`
}

type Result struct {
	Target string `json:"target"`
	Hops   []Hop  `json:"hops"`
	Total  int    `json:"total_hops"`
}

func Run(target string, hub *ws.Hub) (*Result, error) {
	out, err := exec.Command("traceroute", "-n", "-m", "30", "-w", "3", target).CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("traceroute failed: %w", err)
	}

	hops := parseTraceroute(string(out))

	for i := range hops {
		if hops[i].IP != "*" && hops[i].IP != "" {
			geo, err := lookupGeo(hops[i].IP)
			if err == nil {
				hops[i].Geo = geo
			}
		}
		hub.Broadcast(ws.Message{Type: "traceroute_hop", Target: target, Data: hops[i]})
	}

	result := &Result{Target: target, Hops: hops, Total: len(hops)}
	hub.Broadcast(ws.Message{Type: "traceroute_complete", Target: target, Data: result})
	return result, nil
}

func parseTraceroute(output string) []Hop {
	var hops []Hop
	lines := strings.Split(output, "\n")
	re := regexp.MustCompile(`^\s*(\d+)\s+(.+)$`)
	ipRe := regexp.MustCompile(`(\d+\.\d+\.\d+\.\d+)`)
	latRe := regexp.MustCompile(`([\d.]+)\s*ms`)

	for _, line := range lines {
		match := re.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		ttl, _ := strconv.Atoi(match[1])
		rest := match[2]

		if strings.TrimSpace(rest) == "* * *" {
			hops = append(hops, Hop{TTL: ttl, IP: "*", Latency: 0})
			continue
		}

		ips := ipRe.FindAllString(rest, -1)
		lats := latRe.FindAllStringSubmatch(rest, -1)

		ip := "*"
		if len(ips) > 0 {
			ip = ips[0]
		}

		latency := 0.0
		if len(lats) > 0 {
			latency, _ = strconv.ParseFloat(lats[0][1], 64)
		}

		hops = append(hops, Hop{TTL: ttl, IP: ip, Host: ip, Latency: latency})
	}
	return hops
}

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
