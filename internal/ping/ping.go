package ping

import (
	"fmt"
	"time"

	"netprobe/internal/ws"

	probing "github.com/prometheus-community/pro-bing"
)

// Result holds the summary statistics after all pings complete
type Result struct {
	Target      string    `json:"target"`
	PacketsSent int       `json:"packets_sent"`
	PacketsRecv int       `json:"packets_recv"`
	PacketLoss  float64   `json:"packet_loss"`
	MinRtt      float64   `json:"min_rtt_ms"`
	AvgRtt      float64   `json:"avg_rtt_ms"`
	MaxRtt      float64   `json:"max_rtt_ms"`
	Jitter      float64   `json:"jitter_ms"`
	Rtts        []float64 `json:"rtts"`
}

// PacketEvent represents a single received packet streamed in real time
type PacketEvent struct {
	Seq int     `json:"seq"`
	Rtt float64 `json:"rtt_ms"`
	TTL int     `json:"ttl"`
}

// Run sends ICMP pings to the target and streams each reply over WebSocket
func Run(target string, count int, timeout time.Duration, hub *ws.Hub) (*Result, error) {
	pinger, err := probing.NewPinger(target)
	if err != nil {
		return nil, fmt.Errorf("failed to create pinger: %w", err)
	}

	// Configure ping parameters
	pinger.Count = count
	pinger.Timeout = timeout * time.Duration(count+2)
	pinger.Interval = 200 * time.Millisecond
	// Required for Docker containers and Linux without sysctl workaround
	pinger.SetPrivileged(true)

	var rtts []float64

	// Stream each packet as it arrives
	pinger.OnRecv = func(pkt *probing.Packet) {
		rttMs := float64(pkt.Rtt.Microseconds()) / 1000.0
		rtts = append(rtts, rttMs)
		hub.Broadcast(ws.Message{
			Type:   "ping_packet",
			Target: target,
			Data: PacketEvent{
				Seq: pkt.Seq,
				Rtt: rttMs,
				TTL: pkt.TTL,
			},
		})
	}

	if err := pinger.Run(); err != nil {
		return nil, fmt.Errorf("ping failed: %w", err)
	}

	// Collect final statistics
	stats := pinger.Statistics()

	result := &Result{
		Target:      target,
		PacketsSent: stats.PacketsSent,
		PacketsRecv: stats.PacketsRecv,
		PacketLoss:  stats.PacketLoss,
		MinRtt:      float64(stats.MinRtt.Microseconds()) / 1000.0,
		AvgRtt:      float64(stats.AvgRtt.Microseconds()) / 1000.0,
		MaxRtt:      float64(stats.MaxRtt.Microseconds()) / 1000.0,
		Jitter:      float64(stats.StdDevRtt.Microseconds()) / 1000.0,
		Rtts:        rtts,
	}

	// Broadcast the complete summary
	hub.Broadcast(ws.Message{Type: "ping_complete", Target: target, Data: result})
	return result, nil
}
