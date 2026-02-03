//nolint:testpackage
package modelsasservice

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestGatewayValidation(t *testing.T) {
	g := NewWithT(t)

	t.Run("Gateway Validation", func(t *testing.T) {
		t.Run("should accept valid Gateway that exists in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "valid-namespace",
						Name:      "valid-gateway",
					},
				},
			}

			// Create a fake client with the gateway present
			cli := createFakeClientWithGateway("valid-namespace", "valid-gateway")

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should accept empty Gateway (uses defaults) when default gateway exists", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{},
				},
			}

			// Default gateway name is computed as {tenant-name}-gateway
			expectedGatewayName := maas.GetGatewayName()
			expectedGatewayNamespace := componentApi.DefaultGatewayNamespace

			// Create a fake client with the default gateway present
			cli := createFakeClientWithGateway(expectedGatewayNamespace, expectedGatewayName)

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify defaults are used via helper methods (spec remains empty, helpers provide defaults)
			g.Expect(maas.GetGatewayNamespace()).Should(Equal(expectedGatewayNamespace))
			g.Expect(maas.GetGatewayName()).Should(Equal(expectedGatewayName))
		})

		t.Run("should accept Gateway with only namespace specified (uses default name)", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-namespace",
						Name:      "",
					},
				},
			}

			// Default name is {tenant}-gateway, so create gateway with that name
			defaultGatewayName := maas.GetGatewayName()
			cli := createFakeClientWithGateway("custom-namespace", defaultGatewayName)

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify custom namespace is used with default name
			g.Expect(maas.GetGatewayNamespace()).Should(Equal("custom-namespace"))
			g.Expect(maas.GetGatewayName()).Should(Equal(defaultGatewayName))
		})

		t.Run("should accept Gateway with only name specified (uses default namespace)", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "",
						Name:      "custom-gateway",
					},
				},
			}

			// Default namespace is used when not specified
			cli := createFakeClientWithGateway(componentApi.DefaultGatewayNamespace, "custom-gateway")

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify default namespace is used with custom name
			g.Expect(maas.GetGatewayNamespace()).Should(Equal(componentApi.DefaultGatewayNamespace))
			g.Expect(maas.GetGatewayName()).Should(Equal("custom-gateway"))
		})

		t.Run("should reject when specified Gateway does not exist in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "non-existent-namespace",
						Name:      "non-existent-gateway",
					},
				},
			}

			// Create a fake client with NO gateway
			cli := createFakeClientWithoutGateway()

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("gateway non-existent-namespace/non-existent-gateway not found"))
			g.Expect(err.Error()).Should(ContainSubstring("the specified Gateway must exist before enabling ModelsAsService"))
		})

		t.Run("should reject when default Gateway does not exist in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{}, // Uses defaults
				},
			}

			// Create a fake client with NO gateway
			cli := createFakeClientWithoutGateway()

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("not found"))
		})
	})
}

func TestConfigureGatewayNamespaceResources(t *testing.T) {
	g := NewWithT(t)

	t.Run("Configure Gateway AuthPolicy", func(t *testing.T) {
		t.Run("should update AuthPolicy namespace and targetRef when found", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			authPolicy := createAuthPolicy(GatewayAuthPolicyName, "wrong-namespace", "old-gateway")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{authPolicy},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("custom-gateway-ns"))

			// Verify targetRef.name was updated
			targetRefName, found, err := unstructured.NestedString(rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(found).Should(BeTrue())
			g.Expect(targetRefName).Should(Equal("custom-gateway"))
		})

		t.Run("should succeed silently when AuthPolicy is not found in resources", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			// No AuthPolicy in resources
			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should not modify AuthPolicy with different name", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			// AuthPolicy with a different name should not be modified
			otherAuthPolicy := createAuthPolicy("other-auth-policy", "original-namespace", "original-gateway")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{otherAuthPolicy},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was NOT updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("original-namespace"))

			// Verify targetRef.name was NOT updated
			targetRefName, found, err := unstructured.NestedString(rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(found).Should(BeTrue())
			g.Expect(targetRefName).Should(Equal("original-gateway"))
		})

		t.Run("should only modify matching AuthPolicy when multiple resources present", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "new-gateway-ns",
						Name:      "new-gateway",
					},
				},
			}

			// Mix of resources
			gatewayAuthPolicy := createAuthPolicy(GatewayAuthPolicyName, "old-namespace", "old-gateway")
			otherAuthPolicy := createAuthPolicy("other-policy", "keep-namespace", "keep-gateway")
			configMap := &unstructured.Unstructured{}
			configMap.SetAPIVersion("v1")
			configMap.SetKind("ConfigMap")
			configMap.SetName("some-config")
			configMap.SetNamespace("app-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{*configMap, gatewayAuthPolicy, otherAuthPolicy},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// ConfigMap should be unchanged
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("app-namespace"))

			// gateway-auth-policy should be updated
			g.Expect(rr.Resources[1].GetNamespace()).Should(Equal("new-gateway-ns"))
			targetRefName, _, _ := unstructured.NestedString(rr.Resources[1].Object, "spec", "targetRef", "name")
			g.Expect(targetRefName).Should(Equal("new-gateway"))

			// other-policy should be unchanged
			g.Expect(rr.Resources[2].GetNamespace()).Should(Equal("keep-namespace"))
			otherTargetRefName, _, _ := unstructured.NestedString(rr.Resources[2].Object, "spec", "targetRef", "name")
			g.Expect(otherTargetRefName).Should(Equal("keep-gateway"))
		})
	})

	t.Run("Configure Gateway DestinationRule", func(t *testing.T) {
		t.Run("should update DestinationRule namespace when found", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			destinationRule := createDestinationRule(GatewayDestinationRuleName, "wrong-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{destinationRule},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("custom-gateway-ns"))
		})

		t.Run("should not modify DestinationRule with different name", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			// DestinationRule with a different name should not be modified
			otherDestinationRule := createDestinationRule("other-destination-rule", "original-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{otherDestinationRule},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was NOT updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("original-namespace"))
		})
	})

	t.Run("Configure Both AuthPolicy and DestinationRule", func(t *testing.T) {
		t.Run("should update both AuthPolicy and DestinationRule namespaces", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.DefaultModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "target-gateway-ns",
						Name:      "target-gateway",
					},
				},
			}

			authPolicy := createAuthPolicy(GatewayAuthPolicyName, "old-namespace", "old-gateway")
			destinationRule := createDestinationRule(GatewayDestinationRuleName, "old-namespace")
			configMap := &unstructured.Unstructured{}
			configMap.SetAPIVersion("v1")
			configMap.SetKind("ConfigMap")
			configMap.SetName("some-config")
			configMap.SetNamespace("app-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{authPolicy, destinationRule, *configMap},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// AuthPolicy should be updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("target-gateway-ns"))
			targetRefName, _, _ := unstructured.NestedString(rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(targetRefName).Should(Equal("target-gateway"))

			// DestinationRule should be updated
			g.Expect(rr.Resources[1].GetNamespace()).Should(Equal("target-gateway-ns"))

			// ConfigMap should be unchanged
			g.Expect(rr.Resources[2].GetNamespace()).Should(Equal("app-namespace"))
		})
	})
}

// createAuthPolicy creates an unstructured AuthPolicy resource for testing.
func createAuthPolicy(name, namespace, targetGatewayName string) unstructured.Unstructured {
	authPolicy := unstructured.Unstructured{}
	authPolicy.SetGroupVersionKind(gvk.AuthPolicyv1)
	authPolicy.SetName(name)
	authPolicy.SetNamespace(namespace)

	// Set spec.targetRef
	_ = unstructured.SetNestedField(authPolicy.Object, targetGatewayName, "spec", "targetRef", "name")
	_ = unstructured.SetNestedField(authPolicy.Object, "Gateway", "spec", "targetRef", "kind")
	_ = unstructured.SetNestedField(authPolicy.Object, "gateway.networking.k8s.io", "spec", "targetRef", "group")

	return authPolicy
}

// createDestinationRule creates an unstructured DestinationRule resource for testing.
func createDestinationRule(name, namespace string) unstructured.Unstructured {
	destinationRule := unstructured.Unstructured{}
	destinationRule.SetGroupVersionKind(gvk.DestinationRule)
	destinationRule.SetName(name)
	destinationRule.SetNamespace(namespace)

	// Set spec.host (typical for DestinationRule)
	_ = unstructured.SetNestedField(destinationRule.Object, "*.local", "spec", "host")

	return destinationRule
}

// createFakeClientWithGateway creates a fake client with a Gateway resource.
func createFakeClientWithGateway(namespace, name string) client.Client {
	scheme := runtime.NewScheme()
	_ = gwapiv1.Install(scheme)

	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gateway).
		Build()
}

// createFakeClientWithoutGateway creates a fake client with no Gateway resources.
func createFakeClientWithoutGateway() client.Client {
	scheme := runtime.NewScheme()
	_ = gwapiv1.Install(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
}

// =============================================================================
// Multi-Tenant Tests
// =============================================================================

func TestMultiTenantNamespaceDerivation(t *testing.T) {
	g := NewWithT(t)

	t.Run("should derive different namespaces from different CR names", func(t *testing.T) {
		tenants := []struct {
			crName            string
			expectedNamespace string
			expectedTenant    string
		}{
			{"tenant-a", "tenant-a", "tenant-a"},
			{"tenant-b", "tenant-b", "tenant-b"},
			{"production", "production", "production"},
			{"dev-team-1", "dev-team-1", "dev-team-1"},
		}

		for _, tc := range tenants {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: tc.crName,
				},
			}

			g.Expect(maas.GetTenantNamespace()).Should(Equal(tc.expectedNamespace),
				"CR name %q should derive namespace %q", tc.crName, tc.expectedNamespace)
			g.Expect(maas.GetTenantName()).Should(Equal(tc.expectedTenant),
				"CR name %q should derive tenant name %q", tc.crName, tc.expectedTenant)
		}
	})

	t.Run("should derive different gateway names per tenant", func(t *testing.T) {
		tenantA := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
		}
		tenantB := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-b"},
		}

		g.Expect(tenantA.GetGatewayName()).Should(Equal("tenant-a-gateway"))
		g.Expect(tenantB.GetGatewayName()).Should(Equal("tenant-b-gateway"))
		g.Expect(tenantA.GetGatewayName()).ShouldNot(Equal(tenantB.GetGatewayName()),
			"Different tenants should have different default gateway names")
	})
}

func TestMultiTenantResourceIsolation(t *testing.T) {
	g := NewWithT(t)

	t.Run("should configure resources with tenant-specific namespaces", func(t *testing.T) {
		tenantA := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
			Spec: componentApi.ModelsAsServiceSpec{
				GatewayRef: componentApi.GatewayRef{
					Namespace: "gateway-ns",
					Name:      "shared-gateway",
				},
			},
		}

		tenantB := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-b"},
			Spec: componentApi.ModelsAsServiceSpec{
				GatewayRef: componentApi.GatewayRef{
					Namespace: "gateway-ns",
					Name:      "shared-gateway",
				},
			},
		}

		// Verify tenant namespaces are different
		g.Expect(tenantA.GetTenantNamespace()).Should(Equal("tenant-a"))
		g.Expect(tenantB.GetTenantNamespace()).Should(Equal("tenant-b"))
		g.Expect(tenantA.GetTenantNamespace()).ShouldNot(Equal(tenantB.GetTenantNamespace()))

		// Verify gateway namespace is shared (as configured)
		g.Expect(tenantA.GetGatewayNamespace()).Should(Equal("gateway-ns"))
		g.Expect(tenantB.GetGatewayNamespace()).Should(Equal("gateway-ns"))
	})

	t.Run("should create tenant-specific DestinationRule names to avoid conflicts", func(t *testing.T) {
		// When multiple tenants share a gateway namespace, DestinationRules need unique names
		tenantA := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
			Spec: componentApi.ModelsAsServiceSpec{
				GatewayRef: componentApi.GatewayRef{
					Namespace: "shared-gateway-ns",
					Name:      "shared-gateway",
				},
			},
		}

		tenantB := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-b"},
			Spec: componentApi.ModelsAsServiceSpec{
				GatewayRef: componentApi.GatewayRef{
					Namespace: "shared-gateway-ns",
					Name:      "shared-gateway",
				},
			},
		}

		// Create DestinationRules for each tenant
		drA := createDestinationRule(GatewayDestinationRuleName, "placeholder")
		drB := createDestinationRule(GatewayDestinationRuleName, "placeholder")

		rrA := &types.ReconciliationRequest{
			Instance:  tenantA,
			Resources: []unstructured.Unstructured{drA},
		}
		rrB := &types.ReconciliationRequest{
			Instance:  tenantB,
			Resources: []unstructured.Unstructured{drB},
		}

		// Configure gateway namespace resources for each tenant
		err := configureGatewayNamespaceResources(t.Context(), rrA)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = configureGatewayNamespaceResources(t.Context(), rrB)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify DestinationRules have tenant-specific names
		g.Expect(rrA.Resources[0].GetName()).Should(Equal(GatewayDestinationRuleName + "-tenant-a"))
		g.Expect(rrB.Resources[0].GetName()).Should(Equal(GatewayDestinationRuleName + "-tenant-b"))
		g.Expect(rrA.Resources[0].GetName()).ShouldNot(Equal(rrB.Resources[0].GetName()),
			"DestinationRules should have unique names per tenant")

		// Verify both are in the shared gateway namespace
		g.Expect(rrA.Resources[0].GetNamespace()).Should(Equal("shared-gateway-ns"))
		g.Expect(rrB.Resources[0].GetNamespace()).Should(Equal("shared-gateway-ns"))
	})
}

func TestMultiTenantClusterScopedResources(t *testing.T) {
	g := NewWithT(t)

	t.Run("should create tenant-specific ClusterRole names", func(t *testing.T) {
		tenantA := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-a"},
		}
		tenantB := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-b"},
		}

		// Create ClusterRole resources
		crA := &unstructured.Unstructured{}
		crA.SetAPIVersion("rbac.authorization.k8s.io/v1")
		crA.SetKind("ClusterRole")
		crA.SetName("maas-api-role")

		crB := &unstructured.Unstructured{}
		crB.SetAPIVersion("rbac.authorization.k8s.io/v1")
		crB.SetKind("ClusterRole")
		crB.SetName("maas-api-role")

		rrA := &types.ReconciliationRequest{
			Instance:  tenantA,
			Resources: []unstructured.Unstructured{*crA},
		}
		rrB := &types.ReconciliationRequest{
			Instance:  tenantB,
			Resources: []unstructured.Unstructured{*crB},
		}

		// Configure tenant resources (this should rename cluster-scoped resources)
		err := configureTenantResources(t.Context(), rrA)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = configureTenantResources(t.Context(), rrB)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify ClusterRoles have tenant-specific names
		g.Expect(rrA.Resources[0].GetName()).Should(ContainSubstring("tenant-a"))
		g.Expect(rrB.Resources[0].GetName()).Should(ContainSubstring("tenant-b"))
		g.Expect(rrA.Resources[0].GetName()).ShouldNot(Equal(rrB.Resources[0].GetName()),
			"ClusterRoles should have unique names per tenant")
	})
}

func TestAuthenticationOverlaySelection(t *testing.T) {
	g := NewWithT(t)

	t.Run("should select OIDC overlay when OIDC is configured", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-oidc"},
			Spec: componentApi.ModelsAsServiceSpec{
				Authentication: &componentApi.AuthenticationSpec{
					OIDC: &componentApi.OIDCAuthSpec{
						JwksURL: "https://keycloak.example.com/realms/test/protocol/openid-connect/certs",
						Issuer:  "https://keycloak.example.com/realms/test",
					},
				},
			},
		}

		// OIDC is configured when JwksURL is non-empty
		g.Expect(maas.GetOIDCJwksURL()).ShouldNot(BeEmpty())
		g.Expect(maas.IsClusterIdentityAuthEnabled()).Should(BeFalse())
	})

	t.Run("should select SA-only overlay when only clusterIdentities is configured", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-sa"},
			Spec: componentApi.ModelsAsServiceSpec{
				Authentication: &componentApi.AuthenticationSpec{
					ClusterIdentities: &componentApi.ClusterIdentityAuthSpec{},
				},
			},
		}

		g.Expect(maas.GetOIDCJwksURL()).Should(BeEmpty())
		g.Expect(maas.IsClusterIdentityAuthEnabled()).Should(BeTrue())
	})

	t.Run("should support hybrid auth (both OIDC and clusterIdentities)", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-hybrid"},
			Spec: componentApi.ModelsAsServiceSpec{
				Authentication: &componentApi.AuthenticationSpec{
					OIDC: &componentApi.OIDCAuthSpec{
						JwksURL: "https://keycloak.example.com/realms/test/protocol/openid-connect/certs",
						Issuer:  "https://keycloak.example.com/realms/test",
					},
					ClusterIdentities: &componentApi.ClusterIdentityAuthSpec{},
				},
			},
		}

		g.Expect(maas.GetOIDCJwksURL()).ShouldNot(BeEmpty())
		g.Expect(maas.IsClusterIdentityAuthEnabled()).Should(BeTrue())
	})

	t.Run("should handle no authentication configured", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-no-auth"},
			Spec:       componentApi.ModelsAsServiceSpec{},
		}

		g.Expect(maas.GetOIDCJwksURL()).Should(BeEmpty())
		g.Expect(maas.IsClusterIdentityAuthEnabled()).Should(BeFalse())
	})

	t.Run("different tenants can have different auth configurations", func(t *testing.T) {
		tenantOIDC := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-oidc"},
			Spec: componentApi.ModelsAsServiceSpec{
				Authentication: &componentApi.AuthenticationSpec{
					OIDC: &componentApi.OIDCAuthSpec{
						JwksURL: "https://keycloak.example.com/realms/oidc/protocol/openid-connect/certs",
					},
				},
			},
		}

		tenantSA := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: "tenant-sa"},
			Spec: componentApi.ModelsAsServiceSpec{
				Authentication: &componentApi.AuthenticationSpec{
					ClusterIdentities: &componentApi.ClusterIdentityAuthSpec{},
				},
			},
		}

		// Verify different tenants can have different auth modes
		g.Expect(tenantOIDC.GetOIDCJwksURL()).ShouldNot(BeEmpty())
		g.Expect(tenantOIDC.IsClusterIdentityAuthEnabled()).Should(BeFalse())

		g.Expect(tenantSA.GetOIDCJwksURL()).Should(BeEmpty())
		g.Expect(tenantSA.IsClusterIdentityAuthEnabled()).Should(BeTrue())
	})
}
