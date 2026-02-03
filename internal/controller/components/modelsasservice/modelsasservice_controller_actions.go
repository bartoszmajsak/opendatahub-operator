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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// getTenantNamespace returns the tenant namespace for the current ModelsAsService CR.
// This is used by the deployment status action and GC action to scope operations
// to only the tenant's resources, preventing cross-tenant issues in multi-tenant deployments.
func getTenantNamespace(_ context.Context, rr *types.ReconciliationRequest) (string, error) {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return "", fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}
	return maas.GetTenantNamespace(), nil
}

// validateGateway validates the Gateway specification in the ModelsAsService resource.
// It validates that the specified Gateway resource exists in the cluster.
// Defaults are applied via helper methods on the ModelsAsService type.
func validateGateway(ctx context.Context, rr *types.ReconciliationRequest) error {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	log := logf.FromContext(ctx)
	tenantName := maas.GetTenantName()
	gatewayNamespace := maas.GetGatewayNamespace()
	gatewayName := maas.GetGatewayName()

	log.Info("Validating gateway for tenant",
		"tenant", tenantName,
		"gatewayNamespace", gatewayNamespace,
		"gatewayName", gatewayName)

	// Validate that the Gateway exists in the cluster
	if err := validateGatewayExists(ctx, rr, gatewayNamespace, gatewayName); err != nil {
		return err
	}

	return nil
}

// validateGatewayExists checks if a Gateway resource exists in the specified namespace.
func validateGatewayExists(ctx context.Context, rr *types.ReconciliationRequest, namespace, name string) error {
	gateway := &gwapiv1.Gateway{}
	err := rr.Client.Get(ctx, k8stypes.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, gateway)

	if err != nil {
		if k8serr.IsNotFound(err) {
			return fmt.Errorf("gateway %s/%s not found: the specified Gateway must exist before enabling ModelsAsService", namespace, name)
		}
		return fmt.Errorf("failed to check if gateway %s/%s exists: %w", namespace, name, err)
	}

	return nil
}

// ensureTenantNamespace creates the tenant namespace and its NetworkPolicy.
// The namespace name is derived from the ModelsAsService CR name.
// Note: This creates the ODH-consistent NetworkPolicy programmatically.
// The Authorino-specific NetworkPolicy is deployed via manifests.
func ensureTenantNamespace(ctx context.Context, rr *types.ReconciliationRequest) error {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	log := logf.FromContext(ctx)
	tenantNamespace := maas.GetTenantNamespace()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: tenantNamespace,
			Labels: map[string]string{
				"opendatahub.io/managed":           "true",
				"opendatahub.io/maas-tenant":       maas.GetTenantName(),
				"kubernetes.io/metadata.name":      tenantNamespace,
				"pod-security.kubernetes.io/audit": "baseline",
			},
		},
	}

	result, err := controllerutil.CreateOrUpdate(ctx, rr.Client, ns, func() error {
		// Ensure labels are set on existing namespace
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		ns.Labels["opendatahub.io/managed"] = "true"
		ns.Labels["opendatahub.io/maas-tenant"] = maas.GetTenantName()
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to ensure tenant namespace %s: %w", tenantNamespace, err)
	}

	log.Info("Tenant namespace reconciled",
		"namespace", tenantNamespace,
		"result", result)

	// Create ODH-consistent NetworkPolicy (like DSCInitialization does)
	if err := ensureTenantNetworkPolicy(ctx, rr.Client, maas, tenantNamespace); err != nil {
		return fmt.Errorf("failed to ensure tenant NetworkPolicy: %w", err)
	}

	maas.Status.TenantNamespace = tenantNamespace

	return nil
}

// ensureTenantNetworkPolicy creates the ODH-consistent NetworkPolicy for a tenant namespace.
// This is consistent with how DSCInitialization creates NetworkPolicies for ODH namespaces,
// allowing traffic from standard ODH sources (ingress controllers, monitoring, etc.).
// Note: Authorino-specific access is handled by a separate manifest-based NetworkPolicy.
// Note: Skips creation for the ODH application namespace (DSCInitialization handles that).
func ensureTenantNetworkPolicy(ctx context.Context, cli client.Client, maas *componentApi.ModelsAsService, namespace string) error {
	log := logf.FromContext(ctx)

	// Skip NetworkPolicy creation for the ODH application namespace.
	// DSCInitialization already creates and owns the NetworkPolicy there.
	appNamespace := cluster.GetApplicationNamespace()
	if namespace == appNamespace {
		log.V(1).Info("Skipping NetworkPolicy creation for application namespace (managed by DSCInitialization)",
			"namespace", namespace)
		return nil
	}

	np := resources.CreateDefaultNetworkPolicy(namespace,
		resources.WithLabels(map[string]string{
			labels.ODH.Component(componentApi.ModelsAsServiceComponentName): labels.True,
			"opendatahub.io/managed": labels.True,
			TenantLabel:              maas.GetTenantName(),
		}),
	)

	if err := resources.ApplyNetworkPolicy(ctx, cli, np, maas, "modelsasservice-controller"); err != nil {
		return err
	}

	log.V(1).Info("Tenant NetworkPolicy reconciled", "namespace", namespace)
	return nil
}

// initialize sets up the manifests for the ModelsAsService component.
// It selects the appropriate overlay based on the ModelsAsService spec:
//   - OIDC configured: uses odh-oidc overlay (includes keycloak-jwt auth).
//   - OIDC not configured: uses base odh overlay (SA auth only).
func initialize(_ context.Context, rr *types.ReconciliationRequest) error {
	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	// Select overlay based on OIDC configuration
	overlayPath := BaseManifestsSourcePath
	if maas.GetOIDCJwksURL() != "" {
		overlayPath = OIDCManifestsSourcePath
	}

	rr.Manifests = []types.ManifestInfo{
		baseManifestInfo(overlayPath),
	}

	return nil
}

// customizeManifests is a no-op for multi-tenant MaaS.
// All tenant-specific customizations are done in post-render actions
// (configureTenantResources, configureGatewayNamespaceResources) to avoid
// modifying shared files on disk which causes race conditions between tenants.
func customizeManifests(_ context.Context, _ *types.ReconciliationRequest) error {
	// NOTE: Previously this called odhdeploy.ApplyParams to modify params.env,
	// but that shared file causes race conditions in multi-tenant scenarios.
	// All customizations are now done in-memory in post-render actions.
	return nil
}

// configureGatewayNamespaceResources is a post-render action that configures resources
// that must be deployed to the gateway's namespace.
//
// For AuthPolicy (gateway-auth-policy):
// 1. Sets the namespace to match the gateway's namespace.
// 2. Updates spec.targetRef.name to point to the configured gateway name.
// 3. Configures authentication providers (OIDC JWKS URL, SA audience).
//
// For DestinationRule:
// 1. Sets the namespace to match the gateway's namespace.
func configureGatewayNamespaceResources(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	tenantName := maas.GetTenantName()
	gatewayNamespace := maas.GetGatewayNamespace()
	gatewayName := maas.GetGatewayName()

	log.V(4).Info("Configuring gateway namespace resources",
		"tenant", tenantName,
		"gatewayNamespace", gatewayNamespace,
		"gatewayName", gatewayName)

	gatewayAuthPolicyFound := false
	maasApiAuthPolicyFound := false
	destinationRuleFound := false

	for idx := range rr.Resources {
		resource := &rr.Resources[idx]
		resourceGVK := resource.GroupVersionKind()

		switch {
		// Gateway-level AuthPolicy (targets the Gateway itself)
		case resourceGVK == gvk.AuthPolicyv1 && resource.GetName() == GatewayAuthPolicyName:
			gatewayAuthPolicyFound = true
			if err := configureGatewayAuthPolicy(log, maas, resource); err != nil {
				return err
			}

		// MaaS API AuthPolicy (targets the HTTPRoute, handles OIDC + SA auth)
		case resourceGVK == gvk.AuthPolicyv1 && resource.GetName() == MaaSAPIAuthPolicyName:
			maasApiAuthPolicyFound = true
			if err := configureMaaSAPIAuthPolicy(ctx, log, rr.Client, maas, resource); err != nil {
				return err
			}

		case resourceGVK == gvk.DestinationRule && resource.GetName() == GatewayDestinationRuleName:
			destinationRuleFound = true
			configureDestinationRule(log, maas, resource)
		}
	}

	if !gatewayAuthPolicyFound {
		log.V(1).Info("Gateway AuthPolicy not found in rendered resources",
			"expectedName", GatewayAuthPolicyName,
			"expectedGVK", gvk.AuthPolicyv1.String())
	}

	if !maasApiAuthPolicyFound {
		log.V(1).Info("MaaS API AuthPolicy not found in rendered resources",
			"expectedName", MaaSAPIAuthPolicyName,
			"expectedGVK", gvk.AuthPolicyv1.String())
	}

	if !destinationRuleFound {
		log.V(1).Info("DestinationRule not found in rendered resources",
			"expectedName", GatewayDestinationRuleName,
			"expectedGVK", gvk.DestinationRule.String())
	}

	// Update status with resolved gateway reference
	maas.Status.GatewayRef = &componentApi.ResolvedGatewayRef{
		Namespace: gatewayNamespace,
		Name:      gatewayName,
	}

	return nil
}

// configureGatewayAuthPolicy configures the gateway-level AuthPolicy.
// It sets the namespace, targetRef, tier lookup URL, and renames it to be tenant-specific.
func configureGatewayAuthPolicy(log logr.Logger, maas *componentApi.ModelsAsService, resource *unstructured.Unstructured) error {
	gatewayNamespace := maas.GetGatewayNamespace()
	gatewayName := maas.GetGatewayName()
	tenantNamespace := maas.GetTenantNamespace()
	tenantName := maas.GetTenantName()

	// Make the resource name tenant-specific to avoid conflicts when multiple tenants
	// share the same gateway namespace
	newName := fmt.Sprintf("%s-%s", resource.GetName(), tenantName)

	log.V(4).Info("Configuring gateway AuthPolicy",
		"originalName", resource.GetName(),
		"newName", newName,
		"originalNamespace", resource.GetNamespace(),
		"newNamespace", gatewayNamespace,
		"targetGateway", gatewayName,
		"tenantNamespace", tenantNamespace)

	resource.SetName(newName)
	resource.SetNamespace(gatewayNamespace)

	if err := unstructured.SetNestedField(resource.Object, gatewayName, "spec", "targetRef", "name"); err != nil {
		return fmt.Errorf("failed to set spec.targetRef.name on gateway AuthPolicy: %w", err)
	}

	// Set the tier lookup URL with the correct tenant namespace
	// Format: https://maas-api.<tenant-namespace>.svc.cluster.local:8443/v1/tiers/lookup
	tierLookupURL := fmt.Sprintf("https://maas-api.%s.svc.cluster.local:8443/v1/tiers/lookup", tenantNamespace)
	if err := unstructured.SetNestedField(resource.Object, tierLookupURL,
		"spec", "rules", "metadata", "matchedTier", "http", "url"); err != nil {
		log.V(1).Info("Failed to set tier lookup URL - path may not exist in manifest", "error", err)
	}

	// Set the ServiceAccount audience for token review (tenant-specific)
	// Must match what maas-api uses when creating service account tokens: {tenantName}-sa
	gatewayAudience := fmt.Sprintf("%s-sa", tenantName)
	audiences := []interface{}{gatewayAudience}
	if err := unstructured.SetNestedSlice(resource.Object, audiences,
		"spec", "rules", "authentication", "service-accounts", "kubernetesTokenReview", "audiences"); err != nil {
		log.V(1).Info("Failed to set gateway ServiceAccount audience", "error", err)
	} else {
		log.V(4).Info("Set gateway ServiceAccount audience", "audience", gatewayAudience)
	}

	return nil
}

// configureMaaSAPIAuthPolicy configures the MaaS API AuthPolicy with OIDC and SA auth.
func configureMaaSAPIAuthPolicy(ctx context.Context, log logr.Logger, cli client.Client, maas *componentApi.ModelsAsService, resource *unstructured.Unstructured) error {
	tenantName := maas.GetTenantName()

	log.V(4).Info("Configuring MaaS API AuthPolicy",
		"name", resource.GetName(),
		"tenant", tenantName)

	// Configure OIDC JWKS URL if specified
	if jwksURL := maas.GetOIDCJwksURL(); jwksURL != "" {
		log.V(4).Info("Setting OIDC JWKS URL", "jwksUrl", jwksURL)
		if err := unstructured.SetNestedField(resource.Object, jwksURL,
			"spec", "rules", "authentication", "keycloak-jwt", "jwt", "jwksUrl"); err != nil {
			log.V(1).Info("Failed to set OIDC JWKS URL - path may not exist in manifest", "error", err)
		}
	}

	// Configure OIDC issuer validation if specified
	if issuer := maas.GetOIDCIssuer(); issuer != "" {
		log.V(4).Info("Setting OIDC issuer", "issuer", issuer)
		if err := unstructured.SetNestedField(resource.Object, issuer,
			"spec", "rules", "authentication", "keycloak-jwt", "jwt", "issuer"); err != nil {
			log.V(1).Info("Failed to set OIDC issuer - path may not exist in manifest", "error", err)
		}
	}

	// Configure cluster identity audiences if enabled
	if maas.IsClusterIdentityAuthEnabled() {
		audiences := maas.GetClusterIdentityAudiences()

		// Determine the default Kubernetes API server audience:
		// 1. Use explicitly configured value from CR spec if provided
		// 2. Otherwise, auto-discover from cluster's Authentication config (OpenShift)
		// 3. Fall back to the standard Kubernetes default
		defaultAudience := maas.GetClusterIdentityDefaultAudience()
		if defaultAudience == "" {
			defaultAudience = cluster.GetServiceAccountIssuer(ctx, cli)
		}

		log.V(4).Info("Setting cluster identity audiences",
			"defaultAudience", defaultAudience,
			"tenantAudiences", audiences)

		// Build the full audience list: k8s API server audience + tenant audiences
		allAudiences := []string{defaultAudience}
		allAudiences = append(allAudiences, audiences...)

		if err := unstructured.SetNestedStringSlice(resource.Object, allAudiences,
			"spec", "rules", "authentication", "openshift-identities", "kubernetesTokenReview", "audiences"); err != nil {
			log.V(1).Info("Failed to set cluster identity audiences - path may not exist in manifest", "error", err)
		}
	}

	return nil
}

// configureDestinationRule updates the DestinationRule resource to use the correct
// gateway namespace and tenant-specific host. It also renames the resource to be
// tenant-specific to avoid conflicts when multiple tenants share the gateway namespace.
func configureDestinationRule(log logr.Logger, maas *componentApi.ModelsAsService, resource *unstructured.Unstructured) {
	gatewayNamespace := maas.GetGatewayNamespace()
	tenantNamespace := maas.GetTenantNamespace()
	tenantName := maas.GetTenantName()

	// Make the resource name tenant-specific to avoid conflicts when multiple tenants
	// share the same gateway namespace
	newName := fmt.Sprintf("%s-%s", resource.GetName(), tenantName)

	log.V(4).Info("Configuring DestinationRule",
		"originalName", resource.GetName(),
		"newName", newName,
		"originalNamespace", resource.GetNamespace(),
		"newNamespace", gatewayNamespace,
		"tenantNamespace", tenantNamespace)

	resource.SetName(newName)
	resource.SetNamespace(gatewayNamespace)

	// Set the host with the correct tenant namespace
	// Format: maas-api.<tenant-namespace>.svc.cluster.local
	host := fmt.Sprintf("maas-api.%s.svc.cluster.local", tenantNamespace)
	if err := unstructured.SetNestedField(resource.Object, host, "spec", "host"); err != nil {
		log.V(1).Info("Failed to set host on DestinationRule", "error", err)
	}
}

// configureTenantResources is a post-render action that configures resources
// deployed to the tenant namespace (maas-api Deployment, etc.).
// It sets the namespace and adds tenant labels for all resources except those
// that go to the gateway namespace. It also makes cluster-scoped resources
// (ClusterRole, ClusterRoleBinding) tenant-specific to avoid conflicts.
func configureTenantResources(ctx context.Context, rr *types.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	maas, ok := rr.Instance.(*componentApi.ModelsAsService)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelsAsService", rr.Instance)
	}

	tenantName := maas.GetTenantName()
	tenantNamespace := maas.GetTenantNamespace()
	log.V(4).Info("Configuring tenant resources", "tenant", tenantName, "namespace", tenantNamespace)

	// Resources that go to the gateway namespace (handled by configureGatewayNamespaceResources)
	gatewayResources := map[string]bool{
		GatewayAuthPolicyName:      true,
		GatewayDestinationRuleName: true,
	}

	for idx := range rr.Resources {
		resource := &rr.Resources[idx]
		resourceGVK := resource.GroupVersionKind()
		resourceName := resource.GetName()

		// Skip resources that go to gateway namespace
		if gatewayResources[resourceName] {
			continue
		}

		// Add tenant label for proper isolation and GC filtering
		labels := resource.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[TenantLabel] = tenantName
		resource.SetLabels(labels)

		// Handle cluster-scoped RBAC resources - make names tenant-specific
		// to avoid conflicts between tenants
		switch resourceGVK {
		case gvk.ClusterRole:
			configureClusterRole(log, resource, tenantName)
			continue // Don't set namespace for cluster-scoped resources

		case gvk.ClusterRoleBinding:
			if err := configureClusterRoleBinding(log, resource, tenantName, tenantNamespace); err != nil {
				return err
			}
			continue // Don't set namespace for cluster-scoped resources
		}

		// Set tenant namespace for namespaced resources
		if resource.GetNamespace() != tenantNamespace {
			log.V(4).Info("Setting tenant namespace",
				"resource", resourceName,
				"kind", resourceGVK.Kind,
				"oldNamespace", resource.GetNamespace(),
				"newNamespace", tenantNamespace)
			resource.SetNamespace(tenantNamespace)
		}

		// Configure maas-parameters ConfigMap with tenant-specific values
		if resourceGVK == gvk.ConfigMap && resourceName == MaaSParametersConfigMapName {
			configureMaaSParametersConfigMap(log, maas, resource)
		}

		// Configure HTTPRoute with gateway reference
		if resourceGVK == gvk.HTTPRoute {
			if err := configureHTTPRoute(log, maas, resource); err != nil {
				return err
			}
		}
	}

	return nil
}

// configureClusterRole makes the ClusterRole name tenant-specific.
func configureClusterRole(log logr.Logger, resource *unstructured.Unstructured, tenantName string) {
	oldName := resource.GetName()
	newName := oldName + "-" + tenantName
	resource.SetName(newName)
	log.V(4).Info("Made ClusterRole tenant-specific",
		"oldName", oldName,
		"newName", newName,
		"tenant", tenantName)
}

// configureClusterRoleBinding makes the ClusterRoleBinding tenant-specific.
// It updates the name, roleRef, and subjects to use tenant-specific values.
func configureClusterRoleBinding(log logr.Logger, resource *unstructured.Unstructured, tenantName, tenantNamespace string) error {
	oldName := resource.GetName()
	newName := oldName + "-" + tenantName
	resource.SetName(newName)

	// Update roleRef.name to point to tenant-specific ClusterRole
	roleRefName, found, err := unstructured.NestedString(resource.Object, "roleRef", "name")
	if err != nil {
		return fmt.Errorf("failed to get roleRef.name: %w", err)
	}
	if found {
		newRoleRefName := roleRefName + "-" + tenantName
		if err := unstructured.SetNestedField(resource.Object, newRoleRefName, "roleRef", "name"); err != nil {
			return fmt.Errorf("failed to set roleRef.name: %w", err)
		}
	}

	// Update subjects to include the tenant namespace
	subjects, found, err := unstructured.NestedSlice(resource.Object, "subjects")
	if err != nil {
		return fmt.Errorf("failed to get subjects: %w", err)
	}
	if found {
		for i, subj := range subjects {
			subject, ok := subj.(map[string]interface{})
			if !ok {
				continue
			}
			// Set namespace for ServiceAccount subjects
			if subject["kind"] == "ServiceAccount" {
				subject["namespace"] = tenantNamespace
				subjects[i] = subject
			}
		}
		if err := unstructured.SetNestedSlice(resource.Object, subjects, "subjects"); err != nil {
			return fmt.Errorf("failed to set subjects: %w", err)
		}
	}

	log.V(4).Info("Made ClusterRoleBinding tenant-specific",
		"oldName", oldName,
		"newName", newName,
		"tenant", tenantName,
		"namespace", tenantNamespace)

	return nil
}

// configureMaaSParametersConfigMap updates the maas-parameters ConfigMap with
// tenant-specific configuration. This enables proper model discovery by telling
// maas-api which gateway to look for when matching LLMInferenceServices.
func configureMaaSParametersConfigMap(log logr.Logger, maas *componentApi.ModelsAsService, resource *unstructured.Unstructured) {
	tenantName := maas.GetTenantName()
	gatewayName := maas.GetGatewayName()
	gatewayNamespace := maas.GetGatewayNamespace()

	log.V(4).Info("Configuring maas-parameters ConfigMap",
		"name", resource.GetName(),
		"tenant", tenantName,
		"gatewayName", gatewayName,
		"gatewayNamespace", gatewayNamespace)

	// Get existing data or create new map
	data, _, _ := unstructured.NestedStringMap(resource.Object, "data")
	if data == nil {
		data = make(map[string]string)
	}

	// Update tenant and gateway configuration
	data["instance-name"] = tenantName
	data["gateway-name"] = gatewayName
	data["gateway-namespace"] = gatewayNamespace
	data["gateway-sa-audience"] = tenantName + "-sa" // derived from instance-name

	// Set the updated data back
	_ = unstructured.SetNestedStringMap(resource.Object, data, "data")
}

// configureHTTPRoute configures the HTTPRoute with the correct gateway reference.
// This sets the parentRef namespace and name to point to the tenant's gateway.
func configureHTTPRoute(log logr.Logger, maas *componentApi.ModelsAsService, resource *unstructured.Unstructured) error {
	gatewayNamespace := maas.GetGatewayNamespace()
	gatewayName := maas.GetGatewayName()

	log.V(4).Info("Configuring HTTPRoute",
		"name", resource.GetName(),
		"gatewayNamespace", gatewayNamespace,
		"gatewayName", gatewayName)

	// Get parentRefs
	parentRefs, found, err := unstructured.NestedSlice(resource.Object, "spec", "parentRefs")
	if err != nil {
		return fmt.Errorf("failed to get parentRefs from HTTPRoute: %w", err)
	}
	if !found || len(parentRefs) == 0 {
		log.V(4).Info("No parentRefs found in HTTPRoute, skipping")
		return nil
	}

	// Update the first parentRef (gateway reference)
	parentRef, ok := parentRefs[0].(map[string]interface{})
	if !ok {
		return errors.New("failed to cast parentRef to map")
	}

	parentRef["namespace"] = gatewayNamespace
	parentRef["name"] = gatewayName
	parentRefs[0] = parentRef

	if err := unstructured.SetNestedSlice(resource.Object, parentRefs, "spec", "parentRefs"); err != nil {
		return fmt.Errorf("failed to set parentRefs on HTTPRoute: %w", err)
	}

	log.V(4).Info("Configured HTTPRoute parentRef",
		"gatewayNamespace", gatewayNamespace,
		"gatewayName", gatewayName)

	return nil
}
