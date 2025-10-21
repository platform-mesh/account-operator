package v1alpha1

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func SetupAccountWebhookWithManager(mgr ctrl.Manager, denyList []string) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&Account{}).
		WithDefaulter(&AccountDefaulter{}).
		WithValidator(&AccountValidator{DenyList: denyList}).
		Complete()
}

type AccountDefaulter struct{}

// Default implements admission.CustomDefaulter.
func (a *AccountDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	account := obj.(*Account)

	req, err := admission.RequestFromContext(ctx)
	if err != nil {
		return err
	}

	// Only set creator if not already specified
	if account.Spec.Creator == nil || *account.Spec.Creator == "" {
		account.Spec.Creator = &req.UserInfo.Username
	}

	return nil
}

var _ webhook.CustomDefaulter = &AccountDefaulter{}
var _ webhook.CustomValidator = &AccountValidator{}

type AccountValidator struct {
	DenyList []string
}

func (v *AccountValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	account := obj.(*Account)
	if account.Spec.Type != AccountTypeOrg {
		return nil, nil
	}

	if len(strings.TrimSpace(account.Name)) < 3 {
		return nil, fmt.Errorf("organization name %q is too short, must be at least 3 characters", account.Name)
	}

	if slices.Contains(v.DenyList, account.Name) {
		return nil, fmt.Errorf("organization name %q is not allowed", account.Name)
	}

	return nil, nil
}

func (v *AccountValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	account := newObj.(*Account)

	if account.Spec.Type != AccountTypeOrg {
		return nil, nil
	}

	if len(strings.TrimSpace(account.Name)) < 3 {
		return nil, fmt.Errorf("organization name %q is too short, must be at least 3 characters", account.Name)
	}

	if slices.Contains(v.DenyList, account.Name) {
		return nil, fmt.Errorf("organization name %q is not allowed", account.Name)
	}

	return nil, nil
}

func (v *AccountValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
