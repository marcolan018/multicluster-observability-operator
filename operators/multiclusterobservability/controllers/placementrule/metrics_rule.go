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

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-cluster-management/multicluster-observability-operator/operators/multiclusterobservability/pkg/config"
	operatorconfig "github.com/open-cluster-management/multicluster-observability-operator/operators/pkg/config"
)

const (
	ThanosRuleSvc    = "observability-thanos-rule"
	MetricsRuleGroup = "dynamic_collect_rule"
)

var (
	firingAlerts = map[string]map[string]string{}
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

func StartMetricsRuleWatcher(c client.Client) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			checkMetricsRule(c)
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

func checkMetricsRule(c client.Client) {
	client := &http.Client{
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
	resp, err := client.Do(req)
	if err != nil {
		log.Error(err, "Failed to get alerts from thanos rule")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		result := &AlertResult{}
		err = json.NewDecoder(resp.Body).Decode(result)
		if err != nil {
			log.Error(err, "Failed to unmarshall the response")
			return
		}
		log.Info("alert result", "status", result.Status)
		for _, group := range result.Data.Groups {
			log.Info("group", "name", group.Name)
			if group.Name == MetricsRuleGroup {
				for _, rule := range group.Rules {
					log.Info("rule", "name", rule.Name, "state", rule.State)
					if rule.State == "firing" {
						for _, alert := range rule.Alerts {
							log.Info("alert", "state", alert.State, "labels", alert.Labels)
							if alert.State == "firing" {
								update := true
								if firingAlerts[group.Name] == nil {
									firingAlerts[group.Name] = map[string]string{
										alert.Labels["cluster"]: "firing",
									}
								} else if _, ok := firingAlerts[group.Name][alert.Labels["cluster"]]; !ok {
									firingAlerts[group.Name][alert.Labels["cluster"]] = "firing"
								} else {
									update = false
								}
								if update {
									// update allowlist for target cluster
									updateClusterAllowlist(c, alert.Labels["cluster"], rule.Name)
								}
							}
						}
					}
				}
			}
		}
	} else {
		body, _ := ioutil.ReadAll(resp.Body)
		log.Info("Failed to get alerts from thanos rule", "status code", resp.StatusCode, "response body", string(body))
	}
}

func updateClusterAllowlist(c client.Client, cluster string, rule string) {
	metricsList := []string{}
	for _, r := range metricsAllowlist.MetricsRuleList {
		if r.Name == rule {
			metricsList = r.MetricsList
		}
	}
	if len(metricsList) == 0 {
		log.Info("No metrics list found", "cluster", cluster, "rule", rule)
		return
	}
	found := &corev1.ConfigMap{}
	err := c.Get(context.TODO(), types.NamespacedName{
		Namespace: cluster,
		Name:      operatorconfig.AllowlistConfigMapName,
	}, found)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			allowlistCM := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: corev1.SchemeGroupVersion.String(),
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      operatorconfig.AllowlistConfigMapName,
					Namespace: cluster,
				},
				Data: map[string]string{},
			}
			allowlist := &MetricsAllowlist{
				NameList: metricsList,
			}
			data, err := yaml.Marshal(allowlist)
			if err != nil {
				log.Error(err, "Failed to marshal allowlist data")
				return
			}
			allowlistCM.Data["metrics_list.yaml"] = string(data)
			err = c.Create(context.TODO(), allowlistCM)
			if err != nil {
				log.Error(err, "Failed to create allowlist configmap for managed cluster", "cluster", cluster)
			}
		} else {
			log.Error(err, "Failed to get allowlist configmap for managed cluster", "cluster", cluster)
			return
		}
	} else {
		// merge metricslist to existing allowlist
	}
}
