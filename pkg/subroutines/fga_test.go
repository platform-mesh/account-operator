package subroutines_test

import (
	"context"
	"testing"

	kcptypes "github.com/platform-mesh/account-operator/pkg/types"
	pmconfig "github.com/platform-mesh/golang-commons/config"

	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/subroutines"
	"github.com/platform-mesh/account-operator/pkg/subroutines/mocks"
)

type fgaError struct {
	code codes.Code
	msg  string
}

func (e fgaError) Error() string { return e.msg }
func (e fgaError) GRPCStatus() *status.Status {
	return status.New(e.code, e.msg)
}

func newFgaError(c openfgav1.ErrorCode, m string) *fgaError {
	return &fgaError{
		code: codes.Code(c),
		msg:  m,
	}
}

func TestFGASubroutine_GetName(t *testing.T) {
	routine := subroutines.NewFGASubroutine(nil, nil, "", "", "")
	assert.Equal(t, "FGASubroutine", routine.GetName())
}

func TestFGASubroutine_Finalizers(t *testing.T) {
	routine := subroutines.NewFGASubroutine(nil, nil, "", "", "")
	assert.Equal(t, []string{"account.core.platform-mesh.io/fga"}, routine.Finalizers())
}

func TestFGASubroutine_Process(t *testing.T) {
	creator := "test-creator"
	defaultContext := pmconfig.SetConfigInContext(context.Background(), config.OperatorConfig{})
	testCases := []struct {
		name          string
		expectedError bool
		expectedPanic bool
		account       *v1alpha1.Account
		ctx           context.Context
		setupMocks    func(*mocks.OpenFGAServiceClient, *mocks.Client)
	}{
		// no panic without cluster in context anymore
		{
			name: "should_skip_processing_if_subroutine_ran_before",
			ctx:  defaultContext,
			account: &v1alpha1.Account{
				Spec: v1alpha1.AccountSpec{
					Type: v1alpha1.AccountTypeOrg,
				},
				Status: v1alpha1.AccountStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "FGASubroutine_Ready",
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).
					Run(func(args mock.Arguments) {
						account := args.Get(2).(*v1alpha1.AccountInfo)
						*account = v1alpha1.AccountInfo{
							ObjectMeta: metav1.ObjectMeta{
								Name: "root-org",
							},
							Spec: v1alpha1.AccountInfoSpec{
								Organization: v1alpha1.AccountLocation{
									Name:               "root-org",
									GeneratedClusterId: "root-org",
									Path:               "root:platform-mesh:org:root-org",
									URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
									Type:               v1alpha1.AccountTypeOrg,
								},
								Account: v1alpha1.AccountLocation{
									Name:               "root-org",
									GeneratedClusterId: "root-org",
									Path:               "root:platform-mesh:org:root-org",
									URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
									Type:               v1alpha1.AccountTypeOrg,
								},
								FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
							},
						}
					}).Return(nil).Once()
			},
		},
		{
			name:          "should_fail_if_get_store_id_fails",
			ctx:           defaultContext,
			expectedError: true,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {
					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
						},
					}

				}).Return(assert.AnError).Once()
			},
		},
		{
			name:          "should_fail_if_get_parent_account_fails",
			ctx:           defaultContext,
			expectedError: true,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Return(assert.AnError)
			},
		},
		{
			name:          "should_fail_if_write_fails",
			ctx:           defaultContext,
			expectedError: true,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.AccountSpec{
					Type: v1alpha1.AccountTypeAccount,
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name:               "test-account",
								GeneratedClusterId: "test-account-id",
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()
				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).Return(nil, assert.AnError)
			},
		},
		{
			name: "should_ignore_error_if_duplicate_write_error",
			ctx:  defaultContext,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.AccountSpec{
					Type: v1alpha1.AccountTypeAccount,
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name:               "test-account",
								GeneratedClusterId: "test-account-id",
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()
				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).
					Return(nil, newFgaError(openfgav1.ErrorCode_write_failed_due_to_invalid_input, "error"))
			},
		},
		{
			name: "should_succeed",
			ctx:  defaultContext,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.AccountSpec{
					Type: v1alpha1.AccountTypeAccount,
				}},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name:               "test-account",
								GeneratedClusterId: "test-account-id",
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()
				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).
					Return(&openfgav1.WriteResponse{}, nil)
			},
		},
		{
			name: "should_succeed_with_creator_for_sa",
			ctx:  defaultContext,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.AccountSpec{
					Type:    v1alpha1.AccountTypeAccount,
					Creator: ptr.To("system:serviceaccount:some-namespace:some-service-account"),
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name:               "test-account",
								GeneratedClusterId: "test-account-id",
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()
				openFGAServiceClientMock.On("Write", mock.Anything, mock.MatchedBy(func(req *openfgav1.WriteRequest) bool {
					// Check for partial match
					return len(req.Writes.TupleKeys) == 1 && req.Writes.TupleKeys[0].User == "user:system.serviceaccount.some-namespace.some-service-account"
				})).Return(&openfgav1.WriteResponse{}, nil).Once()

				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).
					Return(&openfgav1.WriteResponse{}, nil).Times(2)
			},
		},
		{
			name:          "should_fail_with_creator_in_sa_range",
			ctx:           defaultContext,
			expectedError: true,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.AccountSpec{
					Creator: ptr.To("system.serviceaccount.some-namespace.some-service-account"),
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Return(nil)
			},
		},
		{
			name: "should_succeed_with_creator",
			ctx:  defaultContext,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.AccountSpec{
					Type:    v1alpha1.AccountTypeOrg,
					Creator: &creator,
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name:               "root-org",
								GeneratedClusterId: "root-org",
								Path:               "root:platform-mesh:org:root-org",
								URL:                "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type:               v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name:               "test-account",
								GeneratedClusterId: "test-account-id",
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()

				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).
					Return(&openfgav1.WriteResponse{}, nil)
			},
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			openFGAClient := mocks.NewOpenFGAServiceClient(t)

			clientMock := mocks.NewClient(t)

			if test.setupMocks != nil {
				test.setupMocks(openFGAClient, clientMock)
			}

			routine := subroutines.NewFGASubroutine(clientMock, openFGAClient, "owner", "parent", "account")

			if test.expectedPanic {
				assert.Panics(t, func() {
					_, _ = routine.Process(test.ctx, test.account)
				})
				clientMock.AssertExpectations(t)
			} else {
				_, err := routine.Process(test.ctx, test.account)
				if test.expectedError {
					assert.NotNil(t, err)
				} else {
					assert.Nil(t, err)
				}
				clientMock.AssertExpectations(t)
			}

		})
	}
}

func TestCreatorSubroutine_Finalize(t *testing.T) {
	creator := "test-creator"

	testCases := []struct {
		name          string
		expectedError bool
		account       *v1alpha1.Account
		setupMocks    func(*mocks.OpenFGAServiceClient, *mocks.Client)
	}{
		{
			name:          "should_fail_if_get_store_id_fails",
			expectedError: true,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "test-account")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {
					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
						},
					}

				}).Return(nil).Once()
			},
		},
		{
			name:          "should_fail_if_get_parent_account_fails",
			expectedError: true,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Return(assert.AnError)
			},
		},
		{
			name:          "should_fail_if_write_fails",
			expectedError: true,
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()
				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).
					Return(nil, assert.AnError)

			},
		},
		{
			name: "should_ignore_error_if_duplicate_write_error",
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()
				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).
					Return(nil, newFgaError(openfgav1.ErrorCode_write_failed_due_to_invalid_input, "error"))
			},
		},
		{
			name: "should_succeed",
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.AnythingOfType("*v1alpha1.AccountInfo")).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()

				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).
					Return(&openfgav1.WriteResponse{}, nil)
			},
		},
		{
			name: "should_succeed_with_creator",
			account: &v1alpha1.Account{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-account",
					Namespace: "test-namespace",
				},
				Spec: v1alpha1.AccountSpec{
					Creator: &creator,
				},
			},
			setupMocks: func(openFGAServiceClientMock *mocks.OpenFGAServiceClient, clientMock *mocks.Client) {
				mockGetWorkspaceByName(clientMock, kcptypes.LogicalClusterPhaseReady, "root:platform-mesh:orgs:root-org")
				clientMock.On("Get", mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {

					o := args.Get(2).(*v1alpha1.AccountInfo)
					account := o

					*account = v1alpha1.AccountInfo{
						ObjectMeta: metav1.ObjectMeta{
							Name: "root-org",
						},
						Spec: v1alpha1.AccountInfoSpec{
							Organization: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							ParentAccount: &v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							Account: v1alpha1.AccountLocation{
								Name: "root-org",
								Path: "root:platform-mesh:org:root-org",
								URL:  "http://example.com/clusters/root:platform-mesh:org:root-org",
								Type: v1alpha1.AccountTypeOrg,
							},
							FGA: v1alpha1.FGAInfo{Store: v1alpha1.StoreInfo{Id: "123123"}},
						},
					}

				}).Return(nil).Once()

				openFGAServiceClientMock.On("Write", mock.Anything, mock.Anything).
					Return(&openfgav1.WriteResponse{}, nil).Times(3)
			},
		},
	}
	defaultContext := pmconfig.SetConfigInContext(context.Background(), config.OperatorConfig{})
	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {

			openFGAClient := mocks.NewOpenFGAServiceClient(t)
			k8sClient := mocks.NewClient(t)

			if test.setupMocks != nil {
				test.setupMocks(openFGAClient, k8sClient)
			}

			routine := subroutines.NewFGASubroutine(k8sClient, openFGAClient, "owner", "parent", "account")
			ctx := defaultContext
			_, err := routine.Finalize(ctx, test.account)
			if test.expectedError {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}

		})
	}
}
