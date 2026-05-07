package monitor

import (
	"database/sql"
	"log"
	"time"

	"netprobe/internal/ws"

	probing "github.com/prometheus-community/pro-bing"
)

// Monitor runs periodic pings and stores results for trend data
type Monitor struct {
	hub      *ws.Hub
	database *sql.DB
	targets  []string
}

// New creates a monitor with default targets
func New(hub *ws.Hub, database *sql.DB) *Monitor {
	return &Monitor{
		hub:      hub,
		database: database,
		targets:  []string{"google.com", "cloudflare.com", "amazon.com"},
	}
}

// Start begins the periodic monitoring loop
func (m *Monitor) Start() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, target := range m.targets {
			go m.probe(target)
		}
	}
}

// probe pings a single target and inserts the result
func (m *Monitor) probe(target string) {
	pinger, err := probing.NewPinger(target)
	if err != nil {
		log.Printf("Monitor ping error for %s: %v", target, err)
		return
	}

	pinger.Count = 5
	pinger.Timeout = 10 * time.Second
	pinger.SetPrivileged(true)

	if err := pinger.Run(); err != nil {
		log.Printf("Monitor ping run error for %s: %v", target, err)
		return
	}

	stats := pinger.Statistics()
	avgRtt := float64(stats.AvgRtt.Microseconds()) / 1000.0

	_, err = m.database.Exec(
		"INSERT INTO latency_series (target, avg_rtt, packet_loss) VALUES ($1, $2, $3)",
		target, avgRtt, stats.PacketLoss,
	)
	if err != nil {
		log.Printf("Monitor db insert error: %v", err)
	}
}
