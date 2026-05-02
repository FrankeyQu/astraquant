package commandworker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"nof0-api/internal/config"
	"nof0-api/internal/svc"
	executorpkg "nof0-api/pkg/executor"
	llmpkg "nof0-api/pkg/llm"
	managerpkg "nof0-api/pkg/manager"
)

type Runtime struct {
	cancel    context.CancelFunc
	done      chan struct{}
	llmClient llmpkg.LLMClient
}

func Start(parent context.Context, cfg config.CommandWorkerConf, svcCtx *svc.ServiceContext) (*Runtime, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	mgr, llmClient, err := buildManager(svcCtx)
	if err != nil {
		return nil, err
	}
	worker := managerpkg.NewControlCommandWorker(mgr, svcCtx.ControlCommandRepo).WithBatchSize(cfg.BatchSize)
	ctx, cancel := context.WithCancel(parent)
	r := &Runtime{
		cancel:    cancel,
		done:      make(chan struct{}),
		llmClient: llmClient,
	}
	go r.loop(ctx, worker, cfg.Interval)
	logx.Infof("control command worker started interval=%s batch_size=%d", cfg.Interval.String(), cfg.BatchSize)
	return r, nil
}

func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.done != nil {
		<-r.done
	}
	if r.llmClient != nil {
		_ = r.llmClient.Close()
	}
}

func (r *Runtime) loop(ctx context.Context, worker *managerpkg.ControlCommandWorker, interval time.Duration) {
	defer close(r.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	r.processOnce(ctx, worker)
	for {
		select {
		case <-ctx.Done():
			logx.Info("control command worker stopped")
			return
		case <-ticker.C:
			r.processOnce(ctx, worker)
		}
	}
}

func (r *Runtime) processOnce(ctx context.Context, worker *managerpkg.ControlCommandWorker) {
	result, err := worker.ProcessOnce(ctx)
	if err != nil {
		logx.Errorf("control command worker process error: %v", err)
		return
	}
	if result.Claimed > 0 {
		logx.Infof("control command worker processed claimed=%d completed=%d failed=%d cancelled=%d", result.Claimed, result.Completed, result.Failed, result.Cancelled)
	}
}

func buildManager(svcCtx *svc.ServiceContext) (*managerpkg.Manager, llmpkg.LLMClient, error) {
	if svcCtx == nil {
		return nil, nil, errors.New("control command worker: service context is required")
	}
	if svcCtx.ControlCommandRepo == nil {
		return nil, nil, errors.New("control command worker: ControlCommandRepo is required")
	}
	if svcCtx.ManagerConfig == nil {
		return nil, nil, errors.New("control command worker: Manager config is required")
	}
	if svcCtx.LLMConfig == nil {
		return nil, nil, errors.New("control command worker: LLM config is required")
	}
	if len(svcCtx.ExchangeProviders) == 0 {
		return nil, nil, errors.New("control command worker: exchange providers are required")
	}
	if len(svcCtx.MarketProviders) == 0 {
		return nil, nil, errors.New("control command worker: market providers are required")
	}
	llmClient, err := llmpkg.NewClient(svcCtx.LLMConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("control command worker: create llm client: %w", err)
	}
	var recorder executorpkg.ConversationRecorder
	if rec, ok := svcCtx.ManagerPersistenceService.(executorpkg.ConversationRecorder); ok {
		recorder = rec
	}
	execFactory := managerpkg.NewBasicExecutorFactory(llmClient, recorder)
	var opts []managerpkg.Option
	if svcCtx.TraderConfigRepo != nil {
		opts = append(opts, managerpkg.WithConfigRepo(svcCtx.TraderConfigRepo))
	}
	if svcCtx.TraderRuntimeRepo != nil {
		opts = append(opts, managerpkg.WithRuntimeRepo(svcCtx.TraderRuntimeRepo))
	}
	mgr := managerpkg.NewManager(
		svcCtx.ManagerConfig,
		execFactory,
		svcCtx.ExchangeProviders,
		svcCtx.MarketProviders,
		svcCtx.ManagerPersistenceService,
		opts...,
	)
	traderIDs := make([]string, 0, len(svcCtx.ManagerConfig.Traders))
	for _, traderCfg := range svcCtx.ManagerConfig.Traders {
		if _, err := mgr.RegisterTrader(context.Background(), traderCfg); err != nil {
			_ = llmClient.Close()
			return nil, nil, fmt.Errorf("control command worker: register trader %s: %w", traderCfg.ID, err)
		}
		traderIDs = append(traderIDs, traderCfg.ID)
	}
	if svcCtx.ManagerPersistenceService != nil && len(traderIDs) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := svcCtx.ManagerPersistenceService.HydrateCaches(ctx, traderIDs)
		cancel()
		if err != nil {
			_ = llmClient.Close()
			return nil, nil, fmt.Errorf("control command worker: hydrate caches: %w", err)
		}
	}
	return mgr, llmClient, nil
}
