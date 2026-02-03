//nolint:testpackage
package modelsasservice

import (
	"encoding/json"
	"testing"

	"github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	ctrlTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.ModelsAsServiceComponentName))
}

func TestNewCRObject(t *testing.T) {
	handler := &componentHandler{}
	g := NewWithT(t)

	t.Run("creates CR with correct metadata", func(t *testing.T) {
		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)

		cr := handler.NewCRObject(dsc)
		g.Expect(cr).ShouldNot(BeNil())
		g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.ModelsAsService{}))

		// GatewayRef defaults are applied by API server (kubebuilder) or during reconciliation (validateGateway)
		g.Expect(cr).Should(WithTransform(json.Marshal, And(
			jq.Match(`.metadata.name == "%s"`, componentApi.DefaultModelsAsServiceInstanceName),
			jq.Match(`.kind == "%s"`, componentApi.ModelsAsServiceKind),
			jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
			jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, operatorv1.Managed),
		)))
	})

	t.Run("propagates management state from DSC to ModelsAsService annotations", func(t *testing.T) {
		testCases := []struct {
			name                    string
			inputManagementState    operatorv1.ManagementState
			expectedManagementState operatorv1.ManagementState
		}{
			{"Managed state", operatorv1.Managed, operatorv1.Managed},
			{"Removed state", operatorv1.Removed, operatorv1.Removed},
			{"Unmanaged state defaults to Removed", operatorv1.Unmanaged, operatorv1.Removed},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, tc.inputManagementState)
				cr := handler.NewCRObject(dsc)

				g.Expect(cr).Should(WithTransform(json.Marshal,
					jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, tc.expectedManagementState),
				))
			})
		}
	})
}

func TestIsEnabled(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	testCases := []struct {
		name            string
		kserveState     operatorv1.ManagementState
		maasState       operatorv1.ManagementState
		expectedEnabled func() types.GomegaMatcher
	}{
		{"should be enabled when both KServe and MaaS are managed", operatorv1.Managed, operatorv1.Managed, BeTrue},
		{"should be disabled when KServe not managed", operatorv1.Removed, operatorv1.Managed, BeFalse},
		{"should be disabled when KServe managed but MaaS is not enabled", operatorv1.Managed, operatorv1.Removed, BeFalse},
		{"should be disabled when KServe is unmanaged", operatorv1.Unmanaged, operatorv1.Managed, BeFalse},
		{"should be disabled when both KServe and MaaS are unmanaged", operatorv1.Unmanaged, operatorv1.Unmanaged, BeFalse},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dsc := createDSCWithKServeAndMaaS(tc.kserveState, tc.maasState)
			g.Expect(handler.IsEnabled(dsc)).Should(tc.expectedEnabled())
		})
	}
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &componentHandler{}

	readinessTests := []struct {
		name            string
		maasReady       bool
		expectedStatus  metav1.ConditionStatus
		expectedReason  string
		expectedMessage string
	}{
		{
			name:            "should handle enabled component with ready ModelsAsService CR",
			maasReady:       true,
			expectedStatus:  metav1.ConditionTrue,
			expectedReason:  status.ReadyReason,
			expectedMessage: "Component is ready",
		},
		{
			name:            "should handle enabled component with not ready ModelsAsService CR",
			maasReady:       false,
			expectedStatus:  metav1.ConditionFalse,
			expectedReason:  status.NotReadyReason,
			expectedMessage: "Component is not ready",
		},
	}

	for _, tc := range readinessTests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Managed)
			maas := createModelsAsServiceCR(tc.maasReady)

			cli, err := fakeclient.New(fakeclient.WithObjects(dsc, maas))
			g.Expect(err).ShouldNot(HaveOccurred())

			cs, err := handler.UpdateDSCStatus(ctx, &ctrlTypes.ReconciliationRequest{
				Client:     cli,
				Instance:   dsc,
				Conditions: conditions.NewManager(dsc, ReadyConditionType),
			})

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(cs).Should(Equal(tc.expectedStatus))

			g.Expect(dsc).Should(WithTransform(json.Marshal, And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, tc.expectedStatus),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, tc.expectedReason),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, ReadyConditionType, tc.expectedMessage)),
			))
		})
	}

	t.Run("should handle disabled component (MaaS removed)", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &ctrlTypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})

	t.Run("should handle KServe not managed (MaaS depends on KServe)", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Removed, operatorv1.Managed)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &ctrlTypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, "KServeDisabled"),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("KServe component is not managed")`, ReadyConditionType)),
		))
	})

	t.Run("should handle empty management state as Removed", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithKServeAndMaaS(operatorv1.Managed, "")

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &ctrlTypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})
}

func createDSCWithKServeAndMaaS(kserveState, maasState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc",
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: kserveState,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{
						ModelsAsService: componentApi.DSCModelsAsServiceSpec{
							ManagementState: maasState,
						},
					},
				},
			},
		},
	}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	return dsc
}

func createModelsAsServiceCR(ready bool) *componentApi.ModelsAsService {
	c := &componentApi.ModelsAsService{}
	c.SetGroupVersionKind(componentApi.GroupVersion.WithKind(componentApi.ModelsAsServiceKind))
	c.SetName(componentApi.DefaultModelsAsServiceInstanceName)

	if ready {
		c.Status.Conditions = []common.Condition{{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  status.ReadyReason,
			Message: "Component is ready",
		}}
	} else {
		c.Status.Conditions = []common.Condition{{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: "Component is not ready",
		}}
	}

	return c
}
