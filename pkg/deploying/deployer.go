// Copyright (c) 2020 Red Hat, Inc.

package deploying

import (
	"context"
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("deploying")

type deployerFn func(*unstructured.Unstructured, *unstructured.Unstructured) error

// Deployer is used create or update the resources
type Deployer struct {
	client      client.Client
	deployerFns map[string]deployerFn
}

// NewDeployer inits the deployer
func NewDeployer(client client.Client) *Deployer {
	deployer := &Deployer{client: client}
	deployer.deployerFns = map[string]deployerFn{
		"Deployment":         deployer.updateDeployment,
		"Service":            deployer.updateService,
		"ConfigMap":          deployer.updateConfigMap,
		"Secret":             deployer.updateSecret,
		"ClusterRole":        deployer.updateClusterRole,
		"ClusterRoleBinding": deployer.updateClusterRoleBinding,
	}
	return deployer
}

// Deploy is used to create or update the resources
func (d *Deployer) Deploy(obj *unstructured.Unstructured) error {
	found := &unstructured.Unstructured{}
	found.SetGroupVersionKind(obj.GroupVersionKind())
	err := d.client.Get(context.TODO(), types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Create", "Kind:", obj.GroupVersionKind(), "Name:", obj.GetName())
			return d.client.Create(context.TODO(), obj)
		}
		return err
	}

	deployerFn, ok := d.deployerFns[found.GetKind()]
	if ok {
		return deployerFn(obj, found)
	}
	return nil
}

func (d *Deployer) updateDeployment(desiredObj, runtimeObj *unstructured.Unstructured) error {
	runtimeJSON, _ := runtimeObj.MarshalJSON()
	runtimeDepoly := &appsv1.Deployment{}
	err := json.Unmarshal(runtimeJSON, runtimeDepoly)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal runtime Deployment %s", runtimeObj.GetName()))
	}

	desiredJSON, _ := desiredObj.MarshalJSON()
	desiredDepoly := &appsv1.Deployment{}
	err = json.Unmarshal(desiredJSON, desiredDepoly)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal Deployment %s", runtimeObj.GetName()))
	}

	if !apiequality.Semantic.DeepDerivative(desiredDepoly.Spec, runtimeDepoly.Spec) {
		log.Info("Update", "Kind:", runtimeObj.GroupVersionKind(), "Name:", runtimeObj.GetName())
		return d.client.Update(context.TODO(), desiredDepoly)
	}

	return nil
}

func (d *Deployer) updateService(desiredObj, runtimeObj *unstructured.Unstructured) error {
	runtimeJSON, _ := runtimeObj.MarshalJSON()
	runtimeService := &corev1.Service{}
	err := json.Unmarshal(runtimeJSON, runtimeService)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal runtime Service %s", runtimeObj.GetName()))
	}

	desiredJSON, _ := desiredObj.MarshalJSON()
	desiredService := &corev1.Service{}
	err = json.Unmarshal(desiredJSON, desiredService)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal Service %s", runtimeObj.GetName()))
	}

	if !apiequality.Semantic.DeepDerivative(desiredService.Spec, runtimeService.Spec) {
		log.Info("Update", "Kind:", runtimeObj.GroupVersionKind(), "Name:", runtimeObj.GetName())
		return d.client.Update(context.TODO(), desiredService)
	}

	return nil
}

func (d *Deployer) updateConfigMap(desiredObj, runtimeObj *unstructured.Unstructured) error {
	runtimeJSON, _ := runtimeObj.MarshalJSON()
	runtimeConfigMap := &corev1.ConfigMap{}
	err := json.Unmarshal(runtimeJSON, runtimeConfigMap)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal runtime ConfigMap %s", runtimeObj.GetName()))
	}

	desiredJSON, _ := desiredObj.MarshalJSON()
	desiredConfigMap := &corev1.ConfigMap{}
	err = json.Unmarshal(desiredJSON, desiredConfigMap)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal ConfigMap %s", runtimeObj.GetName()))
	}

	if !apiequality.Semantic.DeepDerivative(desiredConfigMap.Data, runtimeConfigMap.Data) {
		log.Info("Update", "Kind:", runtimeObj.GroupVersionKind(), "Name:", runtimeObj.GetName())
		return d.client.Update(context.TODO(), desiredConfigMap)
	}

	return nil
}

func (d *Deployer) updateSecret(desiredObj, runtimeObj *unstructured.Unstructured) error {
	desiredJSON, _ := desiredObj.MarshalJSON()
	desiredSecret := &corev1.Secret{}
	err := json.Unmarshal(desiredJSON, desiredSecret)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal desired Secret %s", desiredObj.GetName()))
	}

	log.Info("Update", "Kind:", desiredObj.GroupVersionKind(), "Name:", desiredObj.GetName())
	return d.client.Update(context.TODO(), desiredSecret)
}

func (d *Deployer) updateClusterRole(desiredObj, runtimeObj *unstructured.Unstructured) error {
	desiredJSON, _ := desiredObj.MarshalJSON()
	desiredClusterRole := &rbacv1.ClusterRole{}
	err := json.Unmarshal(desiredJSON, desiredClusterRole)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal desired ClusterRole %s", desiredObj.GetName()))
	}

	log.Info("Update", "Kind:", desiredObj.GroupVersionKind(), "Name:", desiredObj.GetName())
	return d.client.Update(context.TODO(), desiredClusterRole)
}

func (d *Deployer) updateClusterRoleBinding(desiredObj, runtimeObj *unstructured.Unstructured) error {
	desiredJSON, _ := desiredObj.MarshalJSON()
	desiredClusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	err := json.Unmarshal(desiredJSON, desiredClusterRoleBinding)
	if err != nil {
		log.Error(err, fmt.Sprintf("Failed to Unmarshal desired ClusterRoleBinding %s", desiredObj.GetName()))
	}

	log.Info("Update", "Kind:", desiredObj.GroupVersionKind(), "Name:", desiredObj.GetName())
	return d.client.Update(context.TODO(), desiredClusterRoleBinding)
}