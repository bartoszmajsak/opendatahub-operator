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

package v1alpha1

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	ModelsAsServiceComponentName = "modelsasservice"
	// DefaultModelsAsServiceInstanceName is the name used by DSC for the default tenant.
	// Additional tenants can be created with different names.
	DefaultModelsAsServiceInstanceName = "opendatahub"
	ModelsAsServiceKind                = "ModelsAsService"

	// Default values for Gateway configuration.
	DefaultGatewayNamespace = "openshift-ingress"
)

// Deprecated: Use DefaultModelsAsServiceInstanceName instead.
// ModelsAsServiceInstanceName is kept for backward compatibility.
var ModelsAsServiceInstanceName = DefaultModelsAsServiceInstanceName

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*ModelsAsService)(nil)

// NOTE: json tags are required. Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z][a-z0-9-]*[a-z0-9]$') || self.metadata.name.matches('^[a-z]$')",message="ModelsAsService name must be a valid namespace identifier (lowercase alphanumeric with hyphens, starting with a letter)"
// +kubebuilder:printcolumn:name="Tenant",type=string,JSONPath=`.metadata.name`,description="Tenant identifier"
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.tenantNamespace`,description="Tenant namespace"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// ModelsAsService is the Schema for the modelsasservice API.
// The metadata.name serves as the tenant identifier and determines the target namespace
// where MaaS resources (maas-api, AuthPolicy) will be deployed.
// Multiple ModelsAsService instances can coexist for multi-tenant deployments.
type ModelsAsService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelsAsServiceSpec   `json:"spec,omitempty"`
	Status ModelsAsServiceStatus `json:"status,omitempty"`
}

// ModelsAsServiceSpec defines the desired state of ModelsAsService.
type ModelsAsServiceSpec struct {
	// GatewayRef references an existing Gateway (gateway.networking.k8s.io/v1) resource
	// where models will be published. The referenced gateway MUST exist before creating
	// this ModelsAsService resource.
	// +optional
	GatewayRef GatewayRef `json:"gatewayRef,omitempty"`

	// Authentication configures identity providers for the MaaS gateway.
	// +optional
	Authentication *AuthenticationSpec `json:"authentication,omitempty"`
}

// GatewayRef references an existing Gateway (gateway.networking.k8s.io/v1) resource.
// The gateway must already exist in the cluster - the controller validates this
// during reconciliation and will fail if the gateway is not found.
//
// Note: For PoC simplicity, this uses a minimal namespace+name reference.
// Future consideration: migrate to corev1.TypedObjectReference for standard API patterns.
type GatewayRef struct {
	// Namespace is the namespace where the Gateway resource resides.
	// +kubebuilder:default="openshift-ingress"
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the Gateway resource.
	// Defaults to "{tenant-name}-gateway" if not specified.
	// +optional
	Name string `json:"name,omitempty"`
}

// AuthenticationSpec configures identity sources for the MaaS gateway.
// At least one identity source should be configured for the tenant to be functional.
// +kubebuilder:validation:XValidation:rule="self.oidc != null || self.clusterIdentities != null",message="at least one authentication method must be configured (oidc or clusterIdentities)"
type AuthenticationSpec struct {
	// OIDC configures OpenID Connect authentication for external identity providers
	// (e.g., Keycloak, Dex, Okta). When configured, clients can authenticate using
	// JWT tokens from the OIDC provider.
	// +optional
	OIDC *OIDCAuthSpec `json:"oidc,omitempty"`

	// ClusterIdentities enables authentication for Kubernetes-native identities.
	// When present, entities can authenticate using tokens validated via the
	// Kubernetes TokenReview API (e.g., projected ServiceAccount tokens from pods,
	// operators, CI jobs, or any workload with a ServiceAccount).
	//
	// The token audience is automatically set to "{tenant-name}-sa".
	//
	// Example:
	//   clusterIdentities: {}
	// +optional
	ClusterIdentities *ClusterIdentityAuthSpec `json:"clusterIdentities,omitempty"`
}

// ClusterIdentityAuthSpec configures authentication for Kubernetes-native identities.
// When present (even if empty), entities can authenticate to MaaS using tokens
// validated via the Kubernetes TokenReview API.
//
// The token audience is automatically set to "{tenant-name}-sa" - this convention
// is required because maas-api infers the audience from the tenant name.
type ClusterIdentityAuthSpec struct {
	// DefaultAudience is the Kubernetes API server's ServiceAccount token audience.
	// This is used as the first audience in the TokenReview validation list.
	//
	// If not specified, the operator will attempt to auto-discover this value from
	// the cluster's Authentication configuration (OpenShift). If discovery fails,
	// it falls back to "https://kubernetes.default.svc" (the standard Kubernetes default).
	//
	// You typically only need to set this if:
	// - Your cluster uses a non-standard ServiceAccount issuer
	// - Auto-discovery is not working (e.g., on non-OpenShift clusters)
	// - You want to explicitly control the audience for security reasons
	//
	// +optional
	DefaultAudience string `json:"defaultAudience,omitempty"`
}

// OIDCAuthSpec configures OpenID Connect (OIDC) authentication.
type OIDCAuthSpec struct {
	// JwksURL is the JSON Web Key Set (JWKS) endpoint for token validation.
	// This URL is used to fetch the public keys for verifying JWT signatures.
	// Example: http://keycloak.keycloak.svc:8080/realms/myrealm/protocol/openid-connect/certs
	// +kubebuilder:validation:Pattern=`^https?://.+`
	// +optional
	JwksURL string `json:"jwksUrl,omitempty"`

	// Issuer is the expected token issuer (iss claim) for validation.
	// If not specified, issuer validation is skipped.
	// +optional
	Issuer string `json:"issuer,omitempty"`
}

// ModelsAsServiceStatus defines the observed state of ModelsAsService.
type ModelsAsServiceStatus struct {
	common.Status `json:",inline"`

	// TenantNamespace is the namespace where tenant resources are deployed.
	// This is derived from the CR's metadata.name.
	// +optional
	TenantNamespace string `json:"tenantNamespace,omitempty"`

	// GatewayRef contains the resolved gateway reference.
	// +optional
	GatewayRef *ResolvedGatewayRef `json:"gatewayRef,omitempty"`
}

// ResolvedGatewayRef contains resolved gateway information.
type ResolvedGatewayRef struct {
	// Namespace of the resolved gateway.
	Namespace string `json:"namespace"`
	// Name of the resolved gateway.
	Name string `json:"name"`
}

// +kubebuilder:object:root=true
// ModelsAsServiceList contains a list of ModelsAsService.
type ModelsAsServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelsAsService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelsAsService{}, &ModelsAsServiceList{})
}

// GetTenantName returns the tenant identifier (same as metadata.name).
func (c *ModelsAsService) GetTenantName() string {
	return c.Name
}

// GetTenantNamespace returns the namespace where tenant resources should be deployed.
// This is the same as the tenant name (metadata.name).
func (c *ModelsAsService) GetTenantNamespace() string {
	return c.Name
}

// GetGatewayName returns the gateway name, applying defaults if not specified.
func (c *ModelsAsService) GetGatewayName() string {
	if c.Spec.GatewayRef.Name != "" {
		return c.Spec.GatewayRef.Name
	}
	return fmt.Sprintf("%s-gateway", c.GetTenantName())
}

// GetGatewayNamespace returns the gateway namespace, applying defaults if not specified.
func (c *ModelsAsService) GetGatewayNamespace() string {
	if c.Spec.GatewayRef.Namespace != "" {
		return c.Spec.GatewayRef.Namespace
	}
	return DefaultGatewayNamespace
}

// GetServiceAccountAudience returns the SA audience for this tenant.
// The audience is always inferred from the tenant name as "{tenant-name}-sa".
// This convention is required because maas-api currently infers the audience
// from the tenant name (see maas-api/internal/token/manager.go).
func (c *ModelsAsService) GetServiceAccountAudience() string {
	return fmt.Sprintf("%s-sa", c.GetTenantName())
}

// IsClusterIdentityAuthEnabled returns whether cluster identity authentication is enabled.
// Returns true if spec.authentication.clusterIdentities is present (even if empty).
func (c *ModelsAsService) IsClusterIdentityAuthEnabled() bool {
	return c.Spec.Authentication != nil && c.Spec.Authentication.ClusterIdentities != nil
}

// GetClusterIdentityAudiences returns the valid audiences for cluster identity tokens.
// Currently returns only the default "{tenant-name}-sa" audience.
func (c *ModelsAsService) GetClusterIdentityAudiences() []string {
	return []string{c.GetServiceAccountAudience()}
}

// GetClusterIdentityDefaultAudience returns the explicitly configured default audience
// for cluster identity tokens, or empty string if not configured.
// When empty, the operator should auto-discover the audience from the cluster.
func (c *ModelsAsService) GetClusterIdentityDefaultAudience() string {
	if c.Spec.Authentication == nil || c.Spec.Authentication.ClusterIdentities == nil {
		return ""
	}
	return c.Spec.Authentication.ClusterIdentities.DefaultAudience
}

// GetOIDCJwksURL returns the OIDC JWKS URL if configured.
func (c *ModelsAsService) GetOIDCJwksURL() string {
	if c.Spec.Authentication == nil || c.Spec.Authentication.OIDC == nil {
		return ""
	}
	return c.Spec.Authentication.OIDC.JwksURL
}

// GetOIDCIssuer returns the expected OIDC issuer if configured.
func (c *ModelsAsService) GetOIDCIssuer() string {
	if c.Spec.Authentication == nil || c.Spec.Authentication.OIDC == nil {
		return ""
	}
	return c.Spec.Authentication.OIDC.Issuer
}

func (c *ModelsAsService) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *ModelsAsService) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *ModelsAsService) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

// DSCModelsAsServiceSpec enables ModelsAsService integration in DataScienceCluster.
// When enabled via DSC, a default "opendatahub" tenant is created.
// Additional tenants can be created by applying ModelsAsService CRs directly.
type DSCModelsAsServiceSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`

	// GatewayRef and Authentication configuration for the default tenant.
	// Only these settings are configurable via DSC; additional tenants
	// with full configuration should be created as separate ModelsAsService CRs.
	ModelsAsServiceSpec `json:",inline"`
}

// DSCModelsAsServiceStatus contains the observed state of the ModelsAsService exposed in the DSC instance.
type DSCModelsAsServiceStatus struct {
	common.ManagementSpec `json:",inline"`
}
