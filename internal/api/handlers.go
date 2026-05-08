package api

import (
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"netprobe/internal/classifier"
	"netprobe/internal/dns"
	"netprobe/internal/ping"
	"netprobe/internal/speedtest"
	"netprobe/internal/traceroute"
	"netprobe/internal/ws"
)

type TargetRequest struct {
	Target   string `json:"target"`
	ClientIP string `json:"client_ip"` // Added to receive real IP from frontend
}

func handlePing(hub *ws.Hub, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		result, err := ping.Run(req.Target, 10, time.Second, hub)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		saveResult(database, "ping", req.Target, result)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func handleTraceroute(hub *ws.Hub, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		// Prefer explicit IP from frontend to bypass Docker NAT
		clientIP := req.ClientIP
		if clientIP == "" {
			clientIP = getClientIP(r) // Fallback
		}

		result, err := traceroute.Run(req.Target, clientIP, hub)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		saveResult(database, "traceroute", req.Target, result)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func handleDNS(hub *ws.Hub, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		result, err := dns.Benchmark(req.Target, hub)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		saveResult(database, "dns", req.Target, result)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func handleSpeedTest(hub *ws.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := speedtest.Run(hub)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func handleDiagnose(hub *ws.Hub, database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req TargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		// Prefer explicit IP from frontend to bypass Docker NAT
		clientIP := req.ClientIP
		if clientIP == "" {
			clientIP = getClientIP(r) // Fallback
		}

		hub.Broadcast(ws.Message{Type: "client_info", Target: req.Target, Data: map[string]string{
			"client_ip": clientIP,
		}})

		pingResult, _ := ping.Run(req.Target, 10, time.Second, hub)
		traceResult, _ := traceroute.Run(req.Target, clientIP, hub)
		dnsResult, _ := dns.Benchmark(req.Target, hub)
		speedResult, _ := speedtest.Run(hub)

		diagnosis := classifier.Classify(pingResult, traceResult, dnsResult, speedResult)

		hub.Broadcast(ws.Message{Type: "diagnosis", Target: req.Target, Data: diagnosis})
		saveResult(database, "diagnosis", req.Target, diagnosis)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(diagnosis)
	}
}

func handleHistory(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := database.Query(
			"SELECT id, test_type, target, result, created_at FROM diagnostic_runs ORDER BY created_at DESC LIMIT 50",
		)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var runs []map[string]interface{}
		for rows.Next() {
			var id int
			var testType, target, result string
			var createdAt time.Time
			rows.Scan(&id, &testType, &target, &result, &createdAt)
			runs = append(runs, map[string]interface{}{
				"id": id, "type": testType, "target": target,
				"result": json.RawMessage(result), "created_at": createdAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(runs)
	}
}

func handleTrends(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("target")
		if target == "" {
			target = "google.com"
		}

		rows, err := database.Query(
			"SELECT avg_rtt, packet_loss, created_at FROM latency_series WHERE target = $1 ORDER BY created_at DESC LIMIT 100",
			target,
		)
		if err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var points []map[string]interface{}
		for rows.Next() {
			var avgRtt, packetLoss float64
			var createdAt time.Time
			rows.Scan(&avgRtt, &packetLoss, &createdAt)
			points = append(points, map[string]interface{}{
				"avg_rtt": avgRtt, "packet_loss": packetLoss, "timestamp": createdAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(points)
	}
}

func saveResult(database *sql.DB, testType, target string, result interface{}) {
	data, _ := json.Marshal(result)
	database.Exec(
		"INSERT INTO diagnostic_runs (test_type, target, result) VALUES ($1, $2, $3)",
		testType, target, string(data),
	)
}

func getClientIP(r *http.Request) string {
	ip := ""
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ip = strings.Split(forwarded, ",")[0]
	} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		ip = realIP
	} else {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip = host
	}

	// Local development fallback
	if ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "192.168.") {
		resp, err := http.Get("https://api64.ipify.org")
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return string(body)
		}
	}

	return ip
}
