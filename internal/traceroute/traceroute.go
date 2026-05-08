package traceroute

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"netprobe/internal/ws"
)

type Hop struct {
	TTL      int      `json:"ttl"`
	IP       string   `json:"ip"`
	Host     string   `json:"host"`
	Latency  float64  `json:"latency_ms"`
	Geo      *GeoInfo `json:"geo,omitempty"`
	Mappable bool     `json:"mappable"`
	IsOrigin bool     `json:"is_origin,omitempty"`
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

var (
	geoCache = make(map[string]*GeoInfo)
	geoMutex sync.RWMutex
)

func Run(target string, clientIP string, hub *ws.Hub) (*Result, error) {
	userGeo, err := lookupGeo(clientIP)

	countryCode := "US"
	if err == nil && userGeo != nil && userGeo.CountryCode != "" {
		countryCode = userGeo.CountryCode
	}

	log.Printf("🌍 Requesting Globalping probe in country: %s (IP: %s)", countryCode, clientIP)

	measurementID, err := startGlobalpingTrace(target, countryCode)
	if err != nil {
		return nil, err
	}

	result := &Result{
		Target: target,
		Hops:   []Hop{},
	}

	// Add synthetic origin node
	if userGeo != nil {
		origin := Hop{
			TTL:      0,
			IP:       clientIP,
			Host:     "Client Origin",
			Latency:  0,
			Geo:      userGeo,
			Mappable: true,
			IsOrigin: true,
		}

		result.Hops = append(result.Hops, origin)

		hub.Broadcast(ws.Message{
			Type:   "traceroute_hop",
			Target: target,
			Data:   origin,
		})
	}

	maxPolls := 20

	for i := 0; i < maxPolls; i++ {
		time.Sleep(1 * time.Second)

		status, rawHops, err := getGlobalpingResults(measurementID)
		if err != nil {
			return nil, err
		}

		log.Printf("📡 Globalping Poll - Status: %s | Hops Received: %d", status, len(rawHops))

		for _, hop := range rawHops {

			if hop.IP == "" || hop.IP == "*" {
				continue
			}

			if isPrivateIP(hop.IP) {
				log.Printf("Skipping private hop: %s", hop.IP)
				continue
			}

			geo, err := lookupGeo(hop.IP)

			if err != nil {
				log.Printf(
					"❌ GEO FAIL | TTL=%d | IP=%s | ERR=%v",
					hop.TTL,
					hop.IP,
					err,
				)

				// still broadcast for debugging
				result.Hops = append(result.Hops, hop)

				hub.Broadcast(ws.Message{
					Type:   "traceroute_hop",
					Target: target,
					Data:   hop,
				})

				continue
			}

			log.Printf(
				"✅ GEO OK | TTL=%d | IP=%s | %s, %s | LAT=%f LON=%f",
				hop.TTL,
				hop.IP,
				geo.City,
				geo.Country,
				geo.Lat,
				geo.Lon,
			)

			hop.Geo = geo
			hop.Mappable = true

			hop.Geo = geo
			hop.Mappable = true

			result.Hops = append(result.Hops, hop)

			hub.Broadcast(ws.Message{
				Type:   "traceroute_hop",
				Target: target,
				Data:   hop,
			})
		}

		if status == "finished" {
			break
		}
	}

	result.Total = len(result.Hops)

	hub.Broadcast(ws.Message{
		Type:   "traceroute_complete",
		Target: target,
		Data:   result,
	})

	return result, nil
}

func startGlobalpingTrace(target, location string) (string, error) {
	reqBody := gpMeasurementRequest{
		Type:   "traceroute",
		Target: target,
		Locations: []gpLocation{
			{Country: location},
		},
	}

	jsonData, _ := json.Marshal(reqBody)

	resp, err := http.Post(
		"https://api.globalping.io/v1/measurements",
		"application/json",
		bytes.NewBuffer(jsonData),
	)

	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("globalping error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}

	return res.ID, nil
}

func getGlobalpingResults(measurementID string) (string, []Hop, error) {
	resp, err := http.Get(
		"https://api.globalping.io/v1/measurements/" + measurementID,
	)

	if err != nil {
		return "", nil, err
	}

	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	log.Printf(
		"🌐 GLOBALPING RAW RESPONSE: %s",
		string(bodyBytes),
	)

	var res struct {
		Status  string `json:"status"`
		Results []struct {
			Result struct {
				Hops []struct {
					ResolvedHostname string `json:"resolvedHostname"`
					ResolvedAddress  string `json:"resolvedAddress"`

					Timings []struct {
						Rtt float64 `json:"rtt"`
					} `json:"timings"`
				} `json:"hops"`
			} `json:"result"`
		} `json:"results"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return "", nil, err
	}

	var parsed []Hop

	if len(res.Results) == 0 {
		return res.Status, parsed, nil
	}

	for i, rawHop := range res.Results[0].Result.Hops {

		if rawHop.ResolvedAddress == "" {
			continue
		}

		latency := 0.0

		if len(rawHop.Timings) > 0 {
			latency = rawHop.Timings[0].Rtt
		}

		parsed = append(parsed, Hop{
			TTL:      i + 1,
			IP:       rawHop.ResolvedAddress,
			Host:     rawHop.ResolvedHostname,
			Latency:  latency,
			Mappable: false,
		})
	}

	log.Printf(
		"✅ PARSED %d HOPS FROM GLOBALPING",
		len(parsed),
	)

	return res.Status, parsed, nil
}

func lookupGeo(ip string) (*GeoInfo, error) {
	geoMutex.RLock()

	if cached, exists := geoCache[ip]; exists {
		geoMutex.RUnlock()
		return cached, nil
	}

	geoMutex.RUnlock()

	resp, err := http.Get(
		fmt.Sprintf("https://ipwho.is/%s", ip),
	)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	var data struct {
		Success     bool    `json:"success"`
		City        string  `json:"city"`
		Country     string  `json:"country"`
		CountryCode string  `json:"country_code"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		Connection  struct {
			ISP string `json:"isp"`
			ASN string `json:"asn"`
		} `json:"connection"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	if !data.Success {
		return nil, fmt.Errorf("geo lookup failed")
	}

	geo := &GeoInfo{
		City:        data.City,
		Country:     data.Country,
		CountryCode: data.CountryCode,
		Lat:         data.Latitude,
		Lon:         data.Longitude,
		ISP:         data.Connection.ISP,
		AS:          data.Connection.ASN,
	}

	geoMutex.Lock()
	geoCache[ip] = geo
	geoMutex.Unlock()

	return geo, nil
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)

	if ip == nil {
		return true
	}

	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}

	for _, cidr := range privateRanges {
		_, subnet, _ := net.ParseCIDR(cidr)

		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}
