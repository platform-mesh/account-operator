package subroutines_test

import (
	"context"
	"testing"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWorkspaceTypeSpecCopy(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	baseOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "org"}}
	baseAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: "account"}}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(baseOrg, baseAcc).Build()

	customAccName := "demo-acc"
	customOrgName := "demo-org"

	ctx := context.Background()

	customAcc := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customAccName}}
	require.NoError(t, c.Create(ctx, customAcc))
	customAcc.Spec = baseAcc.Spec
	require.NoError(t, c.Update(ctx, customAcc))

	customOrg := &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: customOrgName}}
	require.NoError(t, c.Create(ctx, customOrg))
	customOrg.Spec = baseOrg.Spec
	customOrg.Spec.DefaultChildWorkspaceType = &kcptenancyv1alpha.WorkspaceTypeReference{Name: kcptenancyv1alpha.WorkspaceTypeName(customAccName), Path: "root"}
	require.NoError(t, c.Update(ctx, customOrg))

	fetchedOrg := &kcptenancyv1alpha.WorkspaceType{}
	require.Eventually(t, func() bool {
		if err := c.Get(ctx, client.ObjectKey{Name: customOrgName}, fetchedOrg); err != nil {
			return false
		}
		return fetchedOrg.Spec.DefaultChildWorkspaceType != nil && string(fetchedOrg.Spec.DefaultChildWorkspaceType.Name) == customAccName
	}, 2*time.Second, 100*time.Millisecond)
}
