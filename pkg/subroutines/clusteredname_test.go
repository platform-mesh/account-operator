package subroutines

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// dummyRuntimeObject implements runtimeobject.RuntimeObject for testing
type dummyRuntimeObject struct {
	metav1.ObjectMeta
}

func (d *dummyRuntimeObject) GetObjectKind() schema.ObjectKind { return nil }
func (d *dummyRuntimeObject) DeepCopyObject() runtime.Object   { return d }
func (d *dummyRuntimeObject) GetName() string                  { return d.Name }
func (d *dummyRuntimeObject) GetNamespace() string             { return d.Namespace }

// Without KCP context, GetClusteredName returns ok=false
func TestGetClusteredName_WithoutCluster(t *testing.T) {
	ctx := context.Background()
	obj := &dummyRuntimeObject{}
	obj.Name = "foo"
	obj.Namespace = "bar"

	_, ok := GetClusteredName(ctx, obj)
	if ok {
		t.Fatalf("expected ok=false, got true")
	}
}

// MustGetClusteredName should panic without cluster in context

func TestMustGetClusteredName_WithoutCluster(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, got none")
		}
	}()
	ctx := context.Background()
	obj := &dummyRuntimeObject{}
	obj.Name = "foo"
	obj.Namespace = "bar"

	_ = MustGetClusteredName(ctx, obj)
}
