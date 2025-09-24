package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestAccountValidator(t *testing.T) {
	validator := &AccountValidator{}
	assert.NotNil(t, validator)

	// Test ValidateCreate with regular account (no type set)
	account := &Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
	}
	_, err := validator.ValidateCreate(nil, account)
	assert.NoError(t, err)

	// Test ValidateUpdate
	_, err = validator.ValidateUpdate(nil, account, account)
	assert.NoError(t, err)

	// Test ValidateDelete
	_, err = validator.ValidateDelete(nil, account)
	assert.NoError(t, err)
}

func TestAccountValidatorWithOrgType(t *testing.T) {
	validator := &AccountValidator{
		DenyList: []string{"forbidden"},
	}

	// Test with org type and valid name
	account := &Account{
		ObjectMeta: metav1.ObjectMeta{Name: "validorg"},
		Spec: AccountSpec{
			Type: AccountTypeOrg,
		},
	}
	_, err := validator.ValidateCreate(nil, account)
	assert.NoError(t, err)

	// Test with org type and too short name
	shortAccount := &Account{
		ObjectMeta: metav1.ObjectMeta{Name: "ab"},
		Spec: AccountSpec{
			Type: AccountTypeOrg,
		},
	}
	_, err = validator.ValidateCreate(nil, shortAccount)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too short")

	// Test with forbidden name
	forbiddenAccount := &Account{
		ObjectMeta: metav1.ObjectMeta{Name: "forbidden"},
		Spec: AccountSpec{
			Type: AccountTypeOrg,
		},
	}
	_, err = validator.ValidateCreate(nil, forbiddenAccount)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

func TestAccountDefaulter(t *testing.T) {
	defaulter := &AccountDefaulter{}
	assert.NotNil(t, defaulter)

	account := &Account{
		ObjectMeta: metav1.ObjectMeta{Name: "test-account"},
	}

	// Test Default (will panic because no context, which we expect and handle)
	assert.Panics(t, func() {
		defaulter.Default(nil, account)
	})
}

func TestWebhookInterfaces(t *testing.T) {
	// Test that types implement expected interfaces
	var _ webhook.CustomValidator = &AccountValidator{}
	var _ webhook.CustomDefaulter = &AccountDefaulter{}
}

func TestAccountValidatorDenyListLogic(t *testing.T) {
	tests := []struct {
		name     string
		denyList []string
		accName  string
		accType  AccountType
		wantErr  bool
	}{
		{
			name:     "non-org account not affected by deny list",
			denyList: []string{"forbidden"},
			accName:  "forbidden",
			accType:  AccountTypeAccount,
			wantErr:  false,
		},
		{
			name:     "org account with allowed name",
			denyList: []string{"forbidden"},
			accName:  "allowed",
			accType:  AccountTypeOrg,
			wantErr:  false,
		},
		{
			name:     "org account with forbidden name",
			denyList: []string{"forbidden"},
			accName:  "forbidden",
			accType:  AccountTypeOrg,
			wantErr:  true,
		},
		{
			name:     "org account with short name",
			denyList: []string{},
			accName:  "ab",
			accType:  AccountTypeOrg,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				ObjectMeta: metav1.ObjectMeta{Name: tt.accName},
				Spec: AccountSpec{
					Type: tt.accType,
				},
			}

			validator := &AccountValidator{
				DenyList: tt.denyList,
			}

			_, err := validator.ValidateCreate(nil, account)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
