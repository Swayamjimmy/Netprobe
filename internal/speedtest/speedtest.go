package speedtest

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"netprobe/internal/ws"
)

// Result holds the final speed test measurements
type Result struct {
	DownloadMbps float64 `json:"download_mbps"`
	DurationMs   float64 `json:"duration_ms"`
	BytesRead    int64   `json:"bytes_read"`
}

// Progress is streamed to the frontend during download
type Progress struct {
	Percent     int     `json:"percent"`
	CurrentMbps float64 `json:"current_mbps"`
}

// Run downloads a 10MB file from Cloudflare and measures throughput
func Run(hub *ws.Hub) (*Result, error) {
	url := "https://speed.cloudflare.com/__down?bytes=10000000"

	start := time.Now()
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("speed test download failed: %w", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 32*1024)
	var totalBytes int64
	lastReport := start

	for {
		n, err := resp.Body.Read(buf)
		totalBytes += int64(n)

		// Stream progress every 200ms so the UI updates smoothly
		if time.Since(lastReport) > 200*time.Millisecond {
			elapsed := time.Since(start).Seconds()
			currentMbps := (float64(totalBytes) * 8) / (elapsed * 1_000_000)
			percent := int((float64(totalBytes) / 10_000_000) * 100)
			if percent > 100 {
				percent = 100
			}

			hub.Broadcast(ws.Message{
				Type: "speedtest_progress",
				Data: Progress{Percent: percent, CurrentMbps: currentMbps},
			})
			lastReport = time.Now()
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read error: %w", err)
		}
	}

	// Calculate final throughput
	elapsed := time.Since(start)
	mbps := (float64(totalBytes) * 8) / (elapsed.Seconds() * 1_000_000)

	result := &Result{
		DownloadMbps: mbps,
		DurationMs:   float64(elapsed.Milliseconds()),
		BytesRead:    totalBytes,
	}

	hub.Broadcast(ws.Message{Type: "speedtest_complete", Data: result})
	return result, nil
}
