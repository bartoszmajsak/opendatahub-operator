/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// NetworkPolicyOption is a functional option for configuring a NetworkPolicy.
type NetworkPolicyOption func(*networkingv1.NetworkPolicy)

// WithLabels adds labels to the NetworkPolicy.
func WithLabels(l map[string]string) NetworkPolicyOption {
	return func(np *networkingv1.NetworkPolicy) {
		if np.Labels == nil {
			np.Labels = make(map[string]string)
		}
		for k, v := range l {
			np.Labels[k] = v
		}
	}
}

// WithIngressRules appends additional ingress rules to the NetworkPolicy.
func WithIngressRules(rules ...networkingv1.NetworkPolicyIngressRule) NetworkPolicyOption {
	return func(np *networkingv1.NetworkPolicy) {
		np.Spec.Ingress = append(np.Spec.Ingress, rules...)
	}
}

// WithIngressFromNamespace appends an ingress rule allowing traffic from namespaces
// matching the given label key/value.
func WithIngressFromNamespace(labelKey, labelValue string) NetworkPolicyOption {
	return func(np *networkingv1.NetworkPolicy) {
		np.Spec.Ingress = append(np.Spec.Ingress,
			networkingv1.NetworkPolicyIngressRule{From: CreateNetworkPolicyPeer(labelKey, labelValue)})
	}
}

// CreateDefaultNetworkPolicy creates a NetworkPolicy with standard ODH ingress rules.
// This is the standard policy applied to ODH-managed namespaces, allowing traffic from:
// - ODH-managed namespaces (opendatahub.io/generated-namespace)
// - Application namespaces (opendatahub.io/application-namespace)
// - Ingress controllers (network.openshift.io/policy-group: ingress)
// - Host network namespace (kubelet probes)
// - Monitoring namespace
// - Observability operator namespace.
//
// Use functional options to customize:
//
//	CreateDefaultNetworkPolicy(ns, WithLabels(labels), WithIngressFromNamespace("key", "value"))
func CreateDefaultNetworkPolicy(namespace string, opts ...NetworkPolicyOption) *networkingv1.NetworkPolicy {
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespace,
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{From: CreateNetworkPolicyPeer(labels.ODH.OwnedNamespace, labels.True)},
				{From: CreateNetworkPolicyPeer(labels.CustomizedAppNamespace, labels.True)},
				{From: CreateNetworkPolicyPeer("network.openshift.io/policy-group", "ingress")},
				{From: CreateNetworkPolicyPeer("kubernetes.io/metadata.name", "openshift-host-network")},
				{From: CreateNetworkPolicyPeer("kubernetes.io/metadata.name", "openshift-monitoring")},
				{From: CreateNetworkPolicyPeer("kubernetes.io/metadata.name", "openshift-cluster-observability-operator")},
			},
		},
	}

	for _, opt := range opts {
		opt(np)
	}

	return np
}

// CreateNetworkPolicyPeer creates a NetworkPolicyPeer with a namespace selector.
func CreateNetworkPolicyPeer(labelKey, labelValue string) []networkingv1.NetworkPolicyPeer {
	return []networkingv1.NetworkPolicyPeer{{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{labelKey: labelValue},
		},
	}}
}

// ApplyNetworkPolicy applies a NetworkPolicy with the given owner reference.
func ApplyNetworkPolicy(
	ctx context.Context,
	cli client.Client,
	np *networkingv1.NetworkPolicy,
	owner client.Object,
	fieldOwner string,
) error {
	if err := EnsureGroupVersionKind(cli.Scheme(), np); err != nil {
		return fmt.Errorf("unable to set GVK on NetworkPolicy: %w", err)
	}

	if owner != nil {
		if err := controllerutil.SetControllerReference(owner, np, cli.Scheme()); err != nil {
			return fmt.Errorf("unable to set OwnerReference on NetworkPolicy: %w", err)
		}
	}

	if err := Apply(ctx, cli, np, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply NetworkPolicy: %w", err)
	}

	return nil
}
