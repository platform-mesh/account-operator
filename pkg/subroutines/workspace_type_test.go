package subroutines

import (
	"context"
	"testing"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	platformmeshcontext "github.com/platform-mesh/golang-commons/context"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
)

type WorkspaceTypeSubroutineTestSuite struct {
	suite.Suite

	scheme *runtime.Scheme
	log    *logger.Logger
	ctx    context.Context
}

func TestWorkspaceTypeSubroutineTestSuite(t *testing.T) {
	suite.Run(t, new(WorkspaceTypeSubroutineTestSuite))
}

func (suite *WorkspaceTypeSubroutineTestSuite) SetupSuite() {
	suite.scheme = runtime.NewScheme()
	utilruntime.Must(corev1alpha1.AddToScheme(suite.scheme))
	utilruntime.Must(corev1.AddToScheme(suite.scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(suite.scheme))

	var err error
	suite.log, err = logger.New(logger.DefaultConfig())
	suite.Require().NoError(err)

	cfg := config.OperatorConfig{}
	suite.ctx, _, _ = platformmeshcontext.StartContext(suite.log, cfg, 1*time.Minute)
}

func (suite *WorkspaceTypeSubroutineTestSuite) newSubroutine() (*WorkspaceTypeSubroutine, client.Client) {
	cl := fake.NewClientBuilder().WithScheme(suite.scheme).Build()
	return NewWorkspaceTypeSubroutineWithClient(cl), cl
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestProcessCreatesWorkspaceTypesForOrg() {
	subroutine, cl := suite.newSubroutine()

	account := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "test-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}
	res, opErr := subroutine.Process(suite.ctx, account)

	suite.Nil(opErr)
	suite.Zero(res.RequeueAfter)

	orgType := &kcptenancyv1alpha.WorkspaceType{}
	suite.NoError(cl.Get(suite.ctx, client.ObjectKey{Name: "test-org-org"}, orgType))

	accType := &kcptenancyv1alpha.WorkspaceType{}
	suite.NoError(cl.Get(suite.ctx, client.ObjectKey{Name: "test-org-acc"}, accType))
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestProcessSkipsAccounts() {
	subroutine, cl := suite.newSubroutine()

	account := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "test-account"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	res, opErr := subroutine.Process(suite.ctx, account)

	suite.Nil(opErr)
	suite.Zero(res.RequeueAfter)

	suite.True(kerrors.IsNotFound(cl.Get(suite.ctx, client.ObjectKey{Name: "test-account-org"}, &kcptenancyv1alpha.WorkspaceType{})))
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestProcessReturnsErrorWhenClientMissing() {
	subroutine := NewWorkspaceTypeSubroutine(nil, nil)
	account := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "test-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}

	_, opErr := subroutine.Process(suite.ctx, account)
	suite.NotNil(opErr)
	suite.True(opErr.Retry())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalizeDeletesWorkspaceTypes() {
	subroutine, cl := suite.newSubroutine()

	// Seed workspace types
	for _, name := range []string{"test-org-org", "test-org-acc"} {
		suite.Require().NoError(cl.Create(suite.ctx, &kcptenancyv1alpha.WorkspaceType{ObjectMeta: metav1.ObjectMeta{Name: name}}))
	}

	account := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "test-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}

	res, opErr := subroutine.Finalize(suite.ctx, account)

	suite.Nil(opErr)
	suite.Zero(res.RequeueAfter)

	suite.True(kerrors.IsNotFound(cl.Get(suite.ctx, client.ObjectKey{Name: "test-org-org"}, &kcptenancyv1alpha.WorkspaceType{})))
	suite.True(kerrors.IsNotFound(cl.Get(suite.ctx, client.ObjectKey{Name: "test-org-acc"}, &kcptenancyv1alpha.WorkspaceType{})))
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalizeSkipsAccounts() {
	subroutine, cl := suite.newSubroutine()

	account := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "test-account"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}

	res, opErr := subroutine.Finalize(suite.ctx, account)
	suite.Nil(opErr)
	suite.Zero(res.RequeueAfter)

	suite.True(kerrors.IsNotFound(cl.Get(suite.ctx, client.ObjectKey{Name: "test-account-org"}, &kcptenancyv1alpha.WorkspaceType{})))
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalizeReturnsErrorWhenClientMissing() {
	subroutine := NewWorkspaceTypeSubroutine(nil, nil)
	account := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "test-org"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeOrg}}

	_, opErr := subroutine.Finalize(suite.ctx, account)
	suite.NotNil(opErr)
	suite.True(opErr.Retry())
}

func (suite *WorkspaceTypeSubroutineTestSuite) TestFinalizeSkipsForNonOrg() {
	subroutine, _ := suite.newSubroutine()
	account := &corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "test-nonorg"}, Spec: corev1alpha1.AccountSpec{Type: corev1alpha1.AccountTypeAccount}}
	res, opErr := subroutine.Finalize(suite.ctx, account)
	suite.Nil(opErr)
	suite.Zero(res.RequeueAfter)
}

// (intentionally skipped error injection test; covered in separate error suite)
