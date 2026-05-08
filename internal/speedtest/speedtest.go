package speedtest

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"netprobe/internal/ws"
)

type Result struct {
	DownloadMbps float64 `json:"download_mbps"`
	DurationMs   float64 `json:"duration_ms"`
	BytesRead    int64   `json:"bytes_read"`
}

type Progress struct {
	Percent     int     `json:"percent"`
	CurrentMbps float64 `json:"current_mbps"`
}

func Run(hub *ws.Hub) (*Result, error) {

	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	url := "https://speed.cloudflare.com/__down?bytes=5000000"

	start := time.Now()

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf(
			"speedtest download failed: %w",
			err,
		)
	}

	defer resp.Body.Close()

	buf := make([]byte, 32*1024)

	var totalBytes int64

	lastReport := start

	for {
		n, err := resp.Body.Read(buf)

		totalBytes += int64(n)

		if time.Since(lastReport) > 300*time.Millisecond {

			elapsed := time.Since(start).Seconds()

			currentMbps :=
				(float64(totalBytes) * 8) /
					(elapsed * 1_000_000)

			percent :=
				int((float64(totalBytes) / 5_000_000) * 100)

			if percent > 100 {
				percent = 100
			}

			hub.Broadcast(ws.Message{
				Type: "speedtest_progress",
				Data: Progress{
					Percent:     percent,
					CurrentMbps: currentMbps,
				},
			})

			lastReport = time.Now()
		}

		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, err
		}
	}

	elapsed := time.Since(start)

	mbps :=
		(float64(totalBytes) * 8) /
			(elapsed.Seconds() * 1_000_000)

	result := &Result{
		DownloadMbps: mbps,
		DurationMs:   float64(elapsed.Milliseconds()),
		BytesRead:    totalBytes,
	}

	hub.Broadcast(ws.Message{
		Type: "speedtest_complete",
		Data: result,
	})

	return result, nil
}
