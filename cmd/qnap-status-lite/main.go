package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/1945251yyq/qnap-monitor-one/internal/history"
	"github.com/1945251yyq/qnap-monitor-one/internal/monitor"
	"github.com/1945251yyq/qnap-monitor-one/internal/webui"
)

var version = "dev"

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	cfg := monitor.ConfigFromEnv()
	store, err := history.Open(cfg.DataRoot, cfg.HistoryRetention)
	if err != nil {
		log.Fatalf("历史数据库初始化失败: %v", err)
	}
	defer store.Close()

	collector := monitor.NewCollector(cfg)
	webui.Version = version
	server := &http.Server{
		Addr: cfg.ListenAddr, Handler: webui.New(collector, store),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go collector.Run(ctx, func(snapshot monitor.Snapshot) {
		if err := store.Add(snapshot); err != nil {
			log.Printf("写入历史数据失败: %v", err)
		}
	})
	go func() {
		log.Printf("QNAP Status Lite %s 正在监听 %s", version, cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务失败: %v", err)
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
