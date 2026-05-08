package traceroute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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
	City        string  `json:"city"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode,omitempty"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	ISP         string  `json:"isp"`
	AS          string  `json:"as"`
}

type Result struct {
	Target string `json:"target"`
	Hops   []Hop  `json:"hops"`
	Total  int    `json:"total_hops"`
}

type gpMeasurementRequest struct {
	Type      string       `json:"type"`
	Target    string       `json:"target"`
	Locations []gpLocation `json:"locations"`
}

type gpLocation struct {
	Country string `json:"country"`
}

func Run(target string, clientIP string, hub *ws.Hub) (*Result, error) {
	// 1. Determine user location for probe selection
	userGeo, err := lookupGeo(clientIP)
	countryCode := "US" // Default fallback

	if err == nil && userGeo != nil && userGeo.CountryCode != "" {
		countryCode = userGeo.CountryCode
	}

	// 2. Start the remote trace
	measurementID, err := startGlobalpingTrace(target, countryCode)
	if err != nil {
		return nil, fmt.Errorf("failed to start distributed trace: %w", err)
	}

	result := &Result{Target: target}

	// 3. Poll for results until complete
	for {
		time.Sleep(1 * time.Second)

		status, rawHops, err := getGlobalpingResults(measurementID)
		if err != nil {
			return nil, err
		}

		for i := len(result.Hops); i < len(rawHops); i++ {
			hop := rawHops[i]

			if hop.IP != "*" && hop.IP != "" {
				geo, err := lookupGeo(hop.IP)
				if err == nil {
					hop.Geo = geo
				}
			}

			result.Hops = append(result.Hops, hop)
			hub.Broadcast(ws.Message{Type: "traceroute_hop", Target: target, Data: hop})
		}

		if status == "finished" {
			break
		}
	}

	result.Total = len(result.Hops)
	hub.Broadcast(ws.Message{Type: "traceroute_complete", Target: target, Data: result})
	return result, nil
}

func startGlobalpingTrace(target, location string) (string, error) {
	reqBody := gpMeasurementRequest{
		Type:      "traceroute",
		Target:    target,
		Locations: []gpLocation{{Country: location}},
	}

	jsonData, _ := json.Marshal(reqBody)
	resp, err := http.Post("https://api.globalping.io/v1/measurements", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var res struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	return res.ID, nil
}

func getGlobalpingResults(measurementID string) (string, []Hop, error) {
	resp, err := http.Get("https://api.globalping.io/v1/measurements/" + measurementID)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	var res struct {
		Status  string `json:"status"`
		Results []struct {
			Result struct {
				Hops []struct {
					Hop   int `json:"hop"`
					Stats []struct {
						IP  string  `json:"ip"`
						Rtt float64 `json:"rtt"`
					} `json:"stats"`
				} `json:"hops"`
			} `json:"result"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", nil, err
	}

	var parsedHops []Hop
	if len(res.Results) > 0 {
		for _, rawHop := range res.Results[0].Result.Hops {
			ip := "*"
			latency := 0.0
			if len(rawHop.Stats) > 0 {
				ip = rawHop.Stats[0].IP
				latency = rawHop.Stats[0].Rtt
			}
			parsedHops = append(parsedHops, Hop{
				TTL:     rawHop.Hop,
				IP:      ip,
				Host:    ip, // We use IP here since Globalping resolves DNS separately
				Latency: latency,
			})
		}
	}

	return res.Status, parsedHops, nil
}

func lookupGeo(ip string) (*GeoInfo, error) {
	resp, err := http.Get(fmt.Sprintf("http://ip-api.com/json/%s?fields=city,country,countryCode,lat,lon,isp,as", ip))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data GeoInfo
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}
