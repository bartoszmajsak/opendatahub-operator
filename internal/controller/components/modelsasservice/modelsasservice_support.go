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

	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

// maasClientKey is a context key for the MaaS-specific client.
// Context-based injection is used to pass the label-filtered client without
// changing the uniform ComponentHandler interface (all components implement
// NewComponentReconciler(ctx, mgr)). MaaS extracts the client while other
// components ignore it, with graceful fallback to manager's default client.
//
// PoC consideration: For production, evaluate whether this pattern should be
// generalized (e.g., ComponentOptions struct) or if MaaS should have a distinct
// initialization path given its unique multi-tenant cache requirements.
type maasClientKey struct{}

// ContextWithClient returns a new context with the MaaS client.
func ContextWithClient(ctx context.Context, cli client.Client) context.Context {
	return context.WithValue(ctx, maasClientKey{}, cli)
}

// ClientFromContext retrieves the MaaS client from context.
// Returns nil if no client was set.
func ClientFromContext(ctx context.Context) client.Client {
	cli, _ := ctx.Value(maasClientKey{}).(client.Client)
	return cli
}

const (
	ComponentName = componentApi.ModelsAsServiceComponentName

	ReadyConditionType = componentApi.ModelsAsServiceKind + status.ReadySuffix

	// Manifest paths.
	// BaseManifestsSourcePath is the default overlay (SA auth only).
	BaseManifestsSourcePath = "overlays/odh"
	// OIDCManifestsSourcePath is the overlay with OIDC authentication support.
	OIDCManifestsSourcePath = "overlays/odh-oidc"

	// GatewayAuthPolicyName is the name of the AuthPolicy resource that configures
	// authentication for the MaaS gateway. This resource needs to be deployed to
	// the same namespace as the gateway it targets.
	GatewayAuthPolicyName = "gateway-auth-policy"

	// GatewayDestinationRuleName is the name of the DestinationRule resource that
	// configures TLS for the MaaS gateway. This resource needs to be deployed to
	// the same namespace as the gateway it targets.
	GatewayDestinationRuleName = "maas-api-backend-tls"

	// MaaSAPIAuthPolicyName is the name of the AuthPolicy resource that protects
	// the maas-api HTTPRoute. This is separate from the gateway-level auth policy.
	MaaSAPIAuthPolicyName = "maas-api-auth-policy"

	// MaaSParametersConfigMapName is the name of the ConfigMap that holds
	// configuration parameters for maas-api (gateway name, namespace, etc.).
	MaaSParametersConfigMapName = "maas-parameters"

	// TenantLabel is used to label all resources belonging to a specific tenant.
	// This enables proper GC filtering and prevents cross-tenant resource deletion
	// in multi-tenant deployments.
	TenantLabel = "opendatahub.io/maas-tenant"
)

var (
	// Image parameter mappings for manifest substitution.
	imagesMap = map[string]string{
		"maas-api-image": "RELATED_IMAGE_ODH_MAAS_API_IMAGE",
	}

	// extraParamsMap provides default parameters for manifest customization.
	// These are overridden per-tenant during reconciliation.
	extraParamsMap = map[string]string{
		"gateway-namespace": componentApi.DefaultGatewayNamespace,
		"gateway-name":      "opendatahub-gateway", // default for DSC-created instance
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func baseManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       deploy.DefaultManifestPath,
		ContextDir: "maas",
		SourcePath: sourcePath,
	}
}
