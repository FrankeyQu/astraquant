// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package main

import (
	"context"
	"flag"
	"fmt"

	"nof0-api/internal/commandworker"
	"nof0-api/internal/config"
	"nof0-api/internal/handler"
	"nof0-api/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

func main() {
	flag.Parse()

	cfg := config.MustLoad()

	server := rest.MustNewServer(cfg.RestConf)
	defer server.Stop()

	ctx := svc.NewServiceContext(*cfg, cfg.MainPath())
	handler.RegisterHandlers(server, ctx)

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerRuntime, err := commandworker.Start(rootCtx, cfg.CommandWorker, ctx)
	if err != nil {
		panic(err)
	}
	if workerRuntime != nil {
		defer workerRuntime.Stop()
	}

	fmt.Printf("Starting server at %s:%d...\n", cfg.Host, cfg.Port)
	server.Start()
}
