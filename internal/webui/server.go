package webui

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/1945251yyq/qnap-status-lite/internal/history"
	"github.com/1945251yyq/qnap-status-lite/internal/monitor"
)

//go:embed static/*
var assets embed.FS

type Server struct {
	collector *monitor.Collector
	history   *history.Store
}

func New(collector *monitor.Collector, store *history.Store) http.Handler {
	server := &Server{collector: collector, history: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", server.status)
	mux.HandleFunc("/api/history", server.historyPoints)
	mux.HandleFunc("/api/health", server.health)
	mux.HandleFunc("/metrics", server.metrics)
	content, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(content)))
	return securityHeaders(mux)
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.collector.Latest())
}

func (s *Server) historyPoints(w http.ResponseWriter, request *http.Request) {
	hours, _ := strconv.Atoi(request.URL.Query().Get("hours"))
	if hours < 1 || hours > 24*31 {
		hours = 24
	}
	points, err := s.history.Query(time.Now().Add(-time.Duration(hours)*time.Hour), 10000)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.collector.Latest()
	healthy := !snapshot.Timestamp.IsZero() && time.Since(snapshot.Timestamp) < 2*time.Minute
	code := http.StatusOK
	if !healthy {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"ok": healthy, "timestamp": snapshot.Timestamp, "version": Version,
	})
}

func (s *Server) metrics(w http.ResponseWriter, _ *http.Request) {
	snapshot := s.collector.Latest()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP qnap_system_cpu_percent NAS CPU utilization.\n# TYPE qnap_system_cpu_percent gauge\nqnap_system_cpu_percent %.3f\n", snapshot.System.CPUPercent)
	fmt.Fprintf(w, "# HELP qnap_system_memory_percent NAS memory utilization.\n# TYPE qnap_system_memory_percent gauge\nqnap_system_memory_percent %.3f\n", snapshot.System.MemoryPercent)
	fmt.Fprintf(w, "# HELP qnap_system_temperature_celsius NAS system temperature.\n# TYPE qnap_system_temperature_celsius gauge\nqnap_system_temperature_celsius %.3f\n", snapshot.System.Temperature)
	fmt.Fprintf(w, "# HELP qnap_cpu_temperature_celsius NAS CPU temperature.\n# TYPE qnap_cpu_temperature_celsius gauge\nqnap_cpu_temperature_celsius %.3f\n", snapshot.System.CPUTemperature)
	for _, volume := range snapshot.Volumes {
		fmt.Fprintf(w, "qnap_volume_used_percent{name=%q,path=%q} %.3f\n", label(volume.Name), label(volume.Path), volume.Percent)
	}
	for _, network := range snapshot.Networks {
		fmt.Fprintf(w, "qnap_network_receive_bytes_per_second{interface=%q} %.3f\n", label(network.Name), network.RxPerSec)
		fmt.Fprintf(w, "qnap_network_transmit_bytes_per_second{interface=%q} %.3f\n", label(network.Name), network.TxPerSec)
	}
	for _, device := range snapshot.PCIe {
		fmt.Fprintf(w, "qnap_pcie_temperature_celsius{bdf=%q,model=%q} %.3f\n", label(device.BDF), label(device.Model), device.Temperature)
		fmt.Fprintf(w, "qnap_pcie_gpu_percent{bdf=%q,model=%q} %.3f\n", label(device.BDF), label(device.Model), device.GPUPercent)
	}
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("JSON response: %v", err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func label(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(value)
}

var Version = "dev"
