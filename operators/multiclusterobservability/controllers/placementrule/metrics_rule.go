// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package placementrule

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/open-cluster-management/multicluster-observability-operator/operators/multiclusterobservability/pkg/config"
)

const (
	ThanosRuleSvc = "observability-thanos-rule"
)

type AlertResult struct {
	Status string          `json:"status"`
	Data   AlertResultData `json:"data"`
}

type AlertResultData struct {
	Groups []RuleGroup `json:"groups"`
}

type RuleGroup struct {
	Name  string      `json:"name"`
	Rules []AlertRule `json:"rules"`
}

type AlertRule struct {
	Name   string  `json:"name"`
	State  string  `json:"state"`
	Alerts []Alert `json:"alerts"`
}

type Alert struct {
	State  string            `json:"state"`
	Labels map[string]string `json:"labels"`
}

func StartMetricsRuleWatcher() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			checkMetricsRule()
			select {
			case <-ctx.Done():
				log.Info("metrics rule watcher goroutine is stopped.")
				return
			case <-time.After(30 * time.Second):
			}
		}
	}()
	cancel()
}

func checkMetricsRule() {
	c := &http.Client{
		Timeout: time.Second * 10,
	}
	thanoSvc, _ := url.Parse(fmt.Sprintf("http://%s-thanos-rule.%s.svc.cluster.local:10902/api/v1/rules?type=alert",
		config.GetOperandName(config.Observatorium), config.GetDefaultNamespace()))
	req := &http.Request{
		Method: "GET",
		URL:    thanoSvc,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}
	resp, err := c.Do(req)
	if err != nil {
		log.Error(err, "Failed to get alerts from thanos rule")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		result := &AlertResult{}
		err = json.NewDecoder(resp.Body).Decode(result)
		log.Info("alert result", "status", result.Status)
		for _, group := range result.Data.Groups {
			log.Info("group", "name", group.Name)
			for _, rule := range group.Rules {
				log.Info("rule", "name", rule.Name, "state", rule.State)
				for _, alert := range rule.Alerts {
					log.Info("alert", "state", alert.State, "labels", alert.Labels)
				}
			}
		}
		if err != nil {
			log.Error(err, "Failed to unmarshall the response")
			return
		}
	} else {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Info("Failed to get alerts from thanos rule", "status code", resp.StatusCode, "response body", string(body))
	}
}
