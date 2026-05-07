package classifier

import (
	"netprobe/internal/dns"
	"netprobe/internal/ping"
	"netprobe/internal/speedtest"
	"netprobe/internal/traceroute"
)

// Diagnosis is the final verdict explaining what is wrong with the connection
type Diagnosis struct {
	Category    string                 `json:"category"`
	Confidence  float64                `json:"confidence"`
	Explanation string                 `json:"explanation"`
	Details     map[string]interface{} `json:"details"`
}

// Classify analyzes all diagnostic results and returns a root cause diagnosis
func Classify(pingResult *ping.Result, traceResult *traceroute.Result, dnsResult *dns.Result, speedResult *speedtest.Result) *Diagnosis {
	// No data means we cannot classify
	if pingResult == nil {
		return &Diagnosis{Category: "Unknown", Confidence: 0, Explanation: "Insufficient data to classify the issue."}
	}

	// Priority 1: Slow DNS resolution
	if dnsResult != nil && dnsResult.Resolvers[0].AvgMs > 100 {
		return &Diagnosis{
			Category:    "DNS",
			Confidence:  0.85,
			Explanation: "DNS resolution is slow. Your configured resolver is taking too long to look up domain names.",
			Details: map[string]interface{}{
				"slowest_resolver_ms": dnsResult.Resolvers[len(dnsResult.Resolvers)-1].AvgMs,
				"fastest_resolver":    dnsResult.Fastest,
			},
		}
	}

	// Priority 2: High packet loss
	if pingResult.PacketLoss > 5 {
		return &Diagnosis{
			Category:    "Packet Loss",
			Confidence:  0.9,
			Explanation: "Significant packet loss detected. This usually indicates Wi-Fi interference, a congested link, or ISP issues.",
			Details: map[string]interface{}{
				"loss_percent": pingResult.PacketLoss,
				"packets_lost": pingResult.PacketsSent - pingResult.PacketsRecv,
			},
		}
	}

	// Priority 3: Local network issues (high first-hop latency)
	if traceResult != nil && len(traceResult.Hops) > 0 {
		firstHopLatency := traceResult.Hops[0].Latency
		if firstHopLatency > 10 {
			return &Diagnosis{
				Category:    "Wi-Fi",
				Confidence:  0.8,
				Explanation: "High latency on the first hop suggests a local network issue, likely Wi-Fi congestion or a weak signal.",
				Details: map[string]interface{}{
					"first_hop_ms": firstHopLatency,
					"first_hop_ip": traceResult.Hops[0].IP,
				},
			}
		}

		// Priority 4: Mid-path routing bottleneck
		for i := 1; i < len(traceResult.Hops)-1; i++ {
			if traceResult.Hops[i].Latency > 100 && traceResult.Hops[i].Latency > traceResult.Hops[i-1].Latency*3 {
				return &Diagnosis{
					Category:    "Routing",
					Confidence:  0.75,
					Explanation: "A significant latency spike was detected mid-path, suggesting a congested or poorly optimized route.",
					Details: map[string]interface{}{
						"bottleneck_hop": traceResult.Hops[i].TTL,
						"bottleneck_ip":  traceResult.Hops[i].IP,
						"bottleneck_isp": traceResult.Hops[i].Geo,
						"latency_jump":   traceResult.Hops[i].Latency - traceResult.Hops[i-1].Latency,
					},
				}
			}
		}
	}

	// Priority 5: General high latency (ISP congestion)
	if pingResult.AvgRtt > 100 {
		return &Diagnosis{
			Category:    "ISP",
			Confidence:  0.7,
			Explanation: "Overall latency is high without a clear single bottleneck. This often indicates ISP congestion or geographic distance.",
			Details: map[string]interface{}{
				"avg_rtt_ms": pingResult.AvgRtt,
				"jitter_ms":  pingResult.Jitter,
			},
		}
	}

	// Priority 6: Low speed despite good latency (server-side throttling)
	if speedResult != nil && speedResult.DownloadMbps < 5 && pingResult.AvgRtt < 50 {
		return &Diagnosis{
			Category:    "Server-Side",
			Confidence:  0.65,
			Explanation: "Low throughput despite good latency suggests the server is throttling connections or is under heavy load.",
			Details: map[string]interface{}{
				"download_mbps": speedResult.DownloadMbps,
				"latency_ms":    pingResult.AvgRtt,
			},
		}
	}

	// Fallback: Everything looks healthy
	return &Diagnosis{
		Category:    "Healthy",
		Confidence:  0.8,
		Explanation: "No significant issues detected. Your connection looks good.",
		Details: map[string]interface{}{
			"avg_rtt_ms":    pingResult.AvgRtt,
			"packet_loss":   pingResult.PacketLoss,
			"download_mbps": speedResult.DownloadMbps,
		},
	}
}
