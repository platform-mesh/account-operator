package v1alpha1

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAccountValidator_ValidateCreate(t *testing.T) {
	tests := []struct {
		name        string
		account     *Account
		denyList    []string
		expectError bool
		errorMsg    string
	}{
		{
			name: "denied organization name",
			account: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "organization"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: true,
			errorMsg:    `organization name "admin" is not allowed`,
		},
		{
			name: "allowed organization name",
			account: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "my-org"},
				Spec:       AccountSpec{Type: "organization"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: false,
		},
		{
			name: "non-organization account with denied name",
			account: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "personal"},
			},
			denyList:    []string{"admin", "root", "system"},
			expectError: false,
		},
		{
			name: "empty deny list",
			account: &Account{
				ObjectMeta: metav1.ObjectMeta{Name: "admin"},
				Spec:       AccountSpec{Type: "organization"},
			},
			denyList:    []string{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := &AccountValidator{DenyList: tt.denyList}
			_, err := validator.ValidateCreate(context.Background(), tt.account)

			if tt.expectError {
				assert.Error(t, err)
				assert.EqualError(t, err, tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
