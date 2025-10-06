/*
Copyright 2024.

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

package controller

import (
	"slices"
	"testing"

	kcpcorev1alpha "github.com/kcp-dev/kcp/sdk/apis/core/v1alpha1"
	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	lifecyclesubroutine "github.com/platform-mesh/golang-commons/controller/lifecycle/subroutine"
	"github.com/platform-mesh/golang-commons/logger"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
)

// buildTestReconciler constructs a reconciler with provided operator config without needing a multicluster manager.
func buildTestReconciler(t *testing.T, cfg config.OperatorConfig) *AccountReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcpcorev1alpha.AddToScheme(scheme))
	utilruntime.Must(kcptenancyv1alpha.AddToScheme(scheme))

	restCfg := &rest.Config{}
	log, err := logger.New(logger.DefaultConfig())
	if err != nil {
		t.Fatalf("logger init: %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	// Manually instantiate without calling NewAccountReconciler to avoid needing a full mc manager.
	return &AccountReconciler{
		log:         log,
		cfg:         cfg,
		baseConfig:  restCfg,
		scheme:      scheme,
		serverCA:    "",
		subroutines: buildAccountSubroutines(cfg, nil, fakeClient, restCfg, scheme, "", nil),
	}
}

func names(subs []lifecyclesubroutine.Subroutine) []string {
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		out = append(out, s.GetName())
	}
	slices.Sort(out)
	return out
}

func TestBuildSubroutines_AllEnabled(t *testing.T) {
	var cfg config.OperatorConfig
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Subroutines.FGA.Enabled = true
	cfg.Subroutines.FGA.CreatorRelation = "creator"
	cfg.Subroutines.FGA.ParentRelation = "parent"
	cfg.Subroutines.FGA.ObjectType = "account"

	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1alpha1.Account{ObjectMeta: metav1.ObjectMeta{Name: "dummy"}}).Build()
	subs := buildAccountSubroutines(cfg, nil, fakeClient, &rest.Config{}, scheme, "", nil)

	if len(subs) != 4 {
		t.Fatalf("expected 4 subroutines, got %d", len(subs))
	}
	got := names(subs)
	expected := []string{"AccountInfoSubroutine", "FGASubroutine", "WorkspaceSubroutine", "WorkspaceTypeSubroutine"}
	slices.Sort(expected)
	if !slices.Equal(got, expected) {
		t.Fatalf("unexpected subroutine names: %v", got)
	}
}

func TestBuildSubroutines_DisabledAll(t *testing.T) {
	var cfg config.OperatorConfig
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	subs := buildAccountSubroutines(cfg, nil, fakeClient, &rest.Config{}, scheme, "", nil)
	if len(subs) != 0 {
		t.Fatalf("expected 0 subroutines, got %d", len(subs))
	}
}

func TestBuildSubroutines_Partial(t *testing.T) {
	var cfg config.OperatorConfig
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(corev1alpha1.AddToScheme(scheme))
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	subs := buildAccountSubroutines(cfg, nil, fakeClient, &rest.Config{}, scheme, "", nil)
	if len(subs) != 2 {
		t.Fatalf("expected 2 subroutines, got %d", len(subs))
	}
	got := names(subs)
	expected := []string{"AccountInfoSubroutine", "WorkspaceSubroutine"}
	slices.Sort(expected)
	if !slices.Equal(got, expected) {
		t.Fatalf("unexpected names: %v", got)
	}
}
