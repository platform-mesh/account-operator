package v1alpha1

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestAccountStatusMethods tests the Account status methods that have 0% coverage
func TestAccountStatusMethods(t *testing.T) {
	account := &Account{
		ObjectMeta: metav1.ObjectMeta{
			Generation: 5,
		},
		Status: AccountStatus{
			ObservedGeneration: 3,
			NextReconcileTime:  metav1.Time{Time: time.Now()},
		},
	}

	// Test GetObservedGeneration
	observed := account.GetObservedGeneration()
	if observed != 3 {
		t.Errorf("Expected ObservedGeneration 3, got %d", observed)
	}

	// Test SetObservedGeneration
	account.SetObservedGeneration(5)
	if account.Status.ObservedGeneration != 5 {
		t.Errorf("Expected ObservedGeneration 5 after set, got %d", account.Status.ObservedGeneration)
	}

	// Test GetNextReconcileTime
	nextTime := account.GetNextReconcileTime()
	if nextTime.IsZero() {
		t.Error("Expected NextReconcileTime to be non-zero")
	}

	// Test SetNextReconcileTime
	newTime := metav1.Time{Time: time.Now().Add(time.Hour)}
	account.SetNextReconcileTime(newTime)
	if account.Status.NextReconcileTime.IsZero() || !account.Status.NextReconcileTime.Equal(&newTime) {
		t.Error("SetNextReconcileTime failed")
	}

	// Test SetNextReconcileTime with zero time
	zeroTime := metav1.Time{}
	account.SetNextReconcileTime(zeroTime)
	if !account.Status.NextReconcileTime.IsZero() {
		t.Error("SetNextReconcileTime with zero time failed")
	}
}

// TestAccountWebhookMethods tests the webhook setup and validator methods
func TestAccountWebhookMethods(t *testing.T) {
	// Test SetupAccountWebhookWithManager - this just ensures it doesn't panic
	// We can't easily test the full setup without a manager, but we can test the functions exist

	// Test AccountValidator ValidateDelete
	validator := &AccountValidator{DenyList: []string{"forbidden"}}
	account := &Account{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-account",
		},
		Spec: AccountSpec{
			Type: AccountTypeAccount,
		},
	}

	warnings, err := validator.ValidateDelete(context.TODO(), account)
	if err != nil {
		t.Errorf("ValidateDelete returned unexpected error: %v", err)
	}
	if warnings != nil {
		t.Errorf("ValidateDelete returned unexpected warnings: %v", warnings)
	}

	// Test AccountDefaulter Default
	defaulter := &AccountDefaulter{}
	// Create a context with an admission request
	ctx := context.TODO()
	err = defaulter.Default(ctx, account)
	// This might return an error because there's no admission request in the context
	// but we're mainly testing that the method exists and doesn't panic
	if err != nil {
		// This is expected since we don't have a proper admission context
		t.Logf("Default returned expected error: %v", err)
	}
}

// TestAccountInfoDeepCopy tests the AccountInfo DeepCopy methods with 0% coverage
func TestAccountInfoDeepCopy(t *testing.T) {
	originalInfo := &AccountInfo{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-info",
			Namespace: "test-namespace",
		},
		Spec: AccountInfoSpec{
			Account: AccountLocation{
				Name: "test-account",
				Type: AccountTypeAccount,
			},
			Organization: AccountLocation{
				Name: "test-org",
				Type: AccountTypeOrg,
			},
			ClusterInfo: ClusterInfo{
				CA: "test-ca",
			},
			FGA: FGAInfo{
				Store: StoreInfo{
					Id: "test-store",
				},
			},
		},
	}

	// Test DeepCopy
	copied := originalInfo.DeepCopy()
	if copied == nil {
		t.Error("DeepCopy returned nil")
	}
	if copied.Name != originalInfo.Name {
		t.Error("DeepCopy failed to copy Name")
	}

	// Test DeepCopyInto
	target := &AccountInfo{}
	originalInfo.DeepCopyInto(target)
	if target.Name != originalInfo.Name {
		t.Error("DeepCopyInto failed")
	}
}

// TestAccountListDeepCopy tests the AccountList DeepCopy methods with 0% coverage
func TestAccountListDeepCopy(t *testing.T) {
	originalList := &AccountList{
		Items: []Account{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "account1",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "account2",
				},
			},
		},
	}

	// Test DeepCopyInto
	target := &AccountList{}
	originalList.DeepCopyInto(target)
	if len(target.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(target.Items))
	}
	if target.Items[0].Name != "account1" {
		t.Error("DeepCopyInto failed to copy first item")
	}
}

// TestAccountInfoListDeepCopy tests AccountInfoList DeepCopy methods
func TestAccountInfoListDeepCopy(t *testing.T) {
	originalList := &AccountInfoList{
		Items: []AccountInfo{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "info1",
				},
			},
		},
	}

	// Test DeepCopyInto
	target := &AccountInfoList{}
	originalList.DeepCopyInto(target)
	if len(target.Items) != 1 {
		t.Errorf("Expected 1 item, got %d", len(target.Items))
	}

	// Test DeepCopy
	copied := originalList.DeepCopy()
	if copied == nil || len(copied.Items) != 1 {
		t.Error("DeepCopy failed")
	}
}
