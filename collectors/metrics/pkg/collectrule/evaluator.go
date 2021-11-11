// Copyright Contributors to the Open Cluster Management project

package collectrule

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-kit/kit/log"

	"github.com/open-cluster-management/multicluster-observability-operator/collectors/metrics/pkg/forwarder"
	rlogger "github.com/open-cluster-management/multicluster-observability-operator/collectors/metrics/pkg/logger"
	"github.com/open-cluster-management/multicluster-observability-operator/collectors/metrics/pkg/metricsclient"
)

const (
	expireDuration = 10 * time.Minute
)

var (
	config         forwarder.Config
	forwardWorker  *forwarder.Worker
	cancel         context.CancelFunc
	pendingRules   = map[string]time.Duration{}
	firingRules    = map[string]time.Duration{}
	enabledMatches = map[string][]string{}
)

type CollectRule struct {
	Name        string   `json:"name"`
	Expr        string   `json:"expr"`
	Duration    string   `json:"for"`
	MetricsList []string `json:"metrics_list"`
}

type Evaluator struct {
	fromClient *metricsclient.Client
	from       *url.URL

	interval     time.Duration
	collectRules []string

	lock        sync.Mutex
	reconfigure chan struct{}

	logger log.Logger
}

func New(cfg forwarder.Config) (*Evaluator, error) {
	config = forwarder.Config{
		From:          cfg.From,
		ToUpload:      cfg.ToUpload,
		FromToken:     cfg.FromToken,
		FromTokenFile: cfg.FromTokenFile,
		FromCAFile:    cfg.FromCAFile,

		AnonymizeLabels:   cfg.AnonymizeLabels,
		AnonymizeSalt:     cfg.AnonymizeSalt,
		AnonymizeSaltFile: cfg.AnonymizeSaltFile,
		Debug:             cfg.Debug,
		Interval:          30 * time.Second,
		LimitBytes:        cfg.LimitBytes,
		Transformer:       cfg.Transformer,

		Logger: cfg.Logger,
	}
	from := &url.URL{
		Scheme: cfg.From.Scheme,
		Host:   cfg.From.Host,
		Path:   "/api/v1/query",
	}
	evaluator := Evaluator{
		from:         from,
		interval:     cfg.EvaluateInterval,
		collectRules: cfg.CollectRules,
		reconfigure:  make(chan struct{}),
		logger:       log.With(cfg.Logger, "component", "collectrule/evaluator"),
	}

	if evaluator.interval == 0 {
		evaluator.interval = 30 * time.Second
	}

	fromClient, err := forwarder.CreateFromClient(cfg, evaluator.interval, "evaluate_query", cfg.Logger)
	if err != nil {
		return nil, err
	}
	evaluator.fromClient = fromClient

	return &evaluator, nil
}

func (e *Evaluator) Reconfigure(cfg forwarder.Config) error {
	evaluator, err := New(cfg)
	if err != nil {
		return fmt.Errorf("failed to reconfigure: %v", err)
	}

	e.lock.Lock()
	defer e.lock.Unlock()

	e.fromClient = evaluator.fromClient
	e.interval = evaluator.interval
	e.from = evaluator.from
	e.collectRules = evaluator.collectRules

	// Signal a restart to Run func.
	// Do this in a goroutine since we do not care if restarting the Run loop is asynchronous.
	go func() { e.reconfigure <- struct{}{} }()
	return nil
}

func (e *Evaluator) Run(ctx context.Context) {
	for {
		// Ensure that the Worker does not access critical configuration during a reconfiguration.
		e.lock.Lock()
		wait := e.interval
		// The critical section ends here.
		e.lock.Unlock()

		e.evaluate(ctx)

		select {
		// If the context is cancelled, then we're done.
		case <-ctx.Done():
			return
		case <-time.After(wait):
		// We want to be able to interrupt a sleep to immediately apply a new configuration.
		case <-e.reconfigure:
		}
	}
}

func (e *Evaluator) evaluate(ctx context.Context) {
	for _, rule := range e.collectRules {
		var r CollectRule
		err := json.Unmarshal(([]byte)(rule), &r)
		if err != nil {
			rlogger.Log(e.logger, rlogger.Error, "msg", "Input error", "err", err, "rule", rule)
			continue
		}
		duration := 0 * time.Minute
		if r.Duration != "" {
			duration, err = time.ParseDuration(r.Duration)
			if err != nil {
				rlogger.Log(e.logger, rlogger.Error, "msg", "Wrong duration input", "err", err)
				continue
			}
		}

		from := e.from
		from.RawQuery = ""
		v := e.from.Query()
		v.Add("query", r.Expr)
		from.RawQuery = v.Encode()

		req := &http.Request{Method: "GET", URL: from}
		result, err := e.fromClient.RetrievRecordingMetrics(ctx, req, r.Name)
		if err != nil {
			rlogger.Log(e.logger, rlogger.Error, "msg", "Failed to evaluate collect rule", "err", err, "rule", r.Expr)
			continue
		} else {
			if len(result) != 0 {
				if _, fired := firingRules[r.Name]; !fired {
					if duration != 0*time.Minute {
						if val, pending := pendingRules[r.Name]; pending {
							if val+e.interval < duration {
								pendingRules[r.Name] = val + e.interval
								rlogger.Log(e.logger, rlogger.Error, "msg", "Collect rule in pending", "rule", r.Name)
								continue
							}
						} else {
							pendingRules[r.Name] = 0 * time.Minute
							rlogger.Log(e.logger, rlogger.Error, "msg", "Collect rule in pending", "rule", r.Name)
							continue
						}
					}
					firingRules[r.Name] = 0 * time.Minute
					enabledMatches[r.Name] = r.MetricsList
					delete(pendingRules, r.Name)
					err = startWorker()
					if err != nil {
						rlogger.Log(e.logger, rlogger.Error, "Failed to start forwarder to collect metrics", "error", err)
					}
					rlogger.Log(e.logger, rlogger.Info, "msg", "collect rule triggered", "rule", r.Name)
				}

				if firingRules[r.Name] != 0*time.Minute {
					firingRules[r.Name] = 0 * time.Minute
				}
			} else {
				if val, fired := firingRules[r.Name]; fired {
					if val+e.interval > expireDuration {
						rlogger.Log(e.logger, rlogger.Info, "msg", "To disable the collect rule", "rule", r.Name)
						delete(firingRules, r.Name)
						delete(enabledMatches, r.Name)
						if len(enabledMatches) == 0 {
							cancel()
							rlogger.Log(e.logger, rlogger.Info, "msg", "Stop the forwarder")
						}
					} else {
						firingRules[r.Name] = val + e.interval
					}
				}
			}
		}
	}
}

func startWorker() error {

	config.Rules = getMatches()
	if forwardWorker == nil {
		var err error
		forwardWorker, err = forwarder.New(config)
		if err != nil {
			return fmt.Errorf("failed to configure forwarder for additional metrics: %v", err)
		}
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go func() {
			forwardWorker.Run(ctx)
			cancel()
		}()
	} else {
		err := forwardWorker.Reconfigure(config)
		if err != nil {
			return fmt.Errorf("failed to reconfigure forwarder for additional metrics: %v", err)
		}
	}

	return nil
}

func getMatches() []string {
	matches := []string{}
	for _, v := range enabledMatches {
		matches = append(matches, v[:]...)
	}
	return matches
}
