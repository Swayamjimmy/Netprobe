package api

import (
	"database/sql"
	"net/http"

	"netprobe/internal/ws"
)

func NewRouter(hub *ws.Hub, database *sql.DB) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", handleHealth)
	mux.HandleFunc("POST /api/ping", handlePing(hub, database))
	mux.HandleFunc("POST /api/traceroute", handleTraceroute(hub, database))
	mux.HandleFunc("POST /api/dns", handleDNS(hub, database))
	mux.HandleFunc("POST /api/speedtest", handleSpeedTest(hub))
	mux.HandleFunc("POST /api/diagnose", handleDiagnose(hub, database))
	mux.HandleFunc("GET /api/history", handleHistory(database))
	mux.HandleFunc("GET /api/trends", handleTrends(database))
	mux.HandleFunc("GET /ws", hub.HandleWebSocket)

	return corsMiddleware(mux)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"netprobe"}`))
}
