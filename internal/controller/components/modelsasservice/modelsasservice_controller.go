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

package modelsasservice

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// NewComponentReconciler creates a new ModelsAsService controller.
// The controller reconciles ModelsAsService CRs, where each CR represents a tenant.
// The CR name determines the tenant namespace where resources are deployed.
func (s *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	// Use MaaS-specific client if provided via context (has label-filtered cache).
	// Falls back to manager's default client if not set.
	rb := reconciler.ReconcilerFor(mgr, &componentApi.ModelsAsService{})
	if maasClient := ClientFromContext(ctx); maasClient != nil {
		rb = rb.WithClient(maasClient)
	}

	_, err := rb.
		// Core Kubernetes resources deployed by MaaS manifests
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&appsv1.Deployment{}, reconciler.WithPredicates(resources.NewDeploymentPredicate())).
		// RBAC resources
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		// Networking resources
		Owns(&networkingv1.NetworkPolicy{}).
		// Gateway API resources
		Owns(&gwapiv1.HTTPRoute{}).
		// NOTE: Gateway is NOT owned - MaaS validates Gateway exists but doesn't create it.
		// Third-party CRDs that may not be available in all environments.
		OwnsGVK(gvk.AuthPolicyv1, reconciler.Dynamic(reconciler.CrdExists(gvk.AuthPolicyv1))).
		OwnsGVK(gvk.DestinationRule, reconciler.Dynamic(reconciler.CrdExists(gvk.DestinationRule))).
		// NOTE: CRD watch disabled for multi-tenancy - it routes to a fixed instance name.
		// NOTE: ConfigMap watch removed for multi-tenancy support.
		// With cluster-wide cache, a generic ConfigMap watch would trigger
		// reconciliation for ANY ConfigMap deletion cluster-wide.
		// The Owns(&corev1.ConfigMap{}) handles owned ConfigMaps via owner references.
		// Reconciliation actions pipeline:
		// 1. Initialize manifests
		WithAction(initialize).
		// 2. Ensure tenant namespace exists (creates if needed)
		WithAction(ensureTenantNamespace).
		// 3. Validate gateway exists
		WithAction(validateGateway).
		// 4. Customize manifests with tenant-specific params
		WithAction(customizeManifests).
		// 5. Render manifests via kustomize
		WithAction(kustomize.NewAction(
			kustomize.WithLabel(labels.ODH.Component(ComponentName), labels.True),
		)).
		// 6. Configure tenant-namespace resources (maas-api Deployment)
		WithAction(configureTenantResources).
		// 7. Configure gateway-namespace resources (AuthPolicy, DestinationRule)
		WithAction(configureGatewayNamespaceResources).
		// 8. Deploy resources to cluster
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		// 9. Update deployment status
		WithAction(deployments.NewAction()).
		// 10. Garbage collect orphaned resources in the tenant namespace.
		// NOTE: GC is temporarily disabled to debug reconciliation loop.
		// TODO: Re-enable once loop is resolved.
		// WithAction(gc.NewAction(
		// 	gc.InNamespaceFn(getTenantNamespace),
		// )).
		// Declares additional conditions contributing to readiness status
		WithConditions(conditionTypes...).
		Build(ctx)
	if err != nil {
		return fmt.Errorf("could not create the ModelsAsService controller: %w", err)
	}

	return nil
}
