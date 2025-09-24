package subroutines

import (
	"context"
	"strings"
	"time"

	kcptenancyv1alpha "github.com/kcp-dev/kcp/sdk/apis/tenancy/v1alpha1"
	"github.com/kcp-dev/logicalcluster/v3"
	commonconfig "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/controller/lifecycle/runtimeobject"
	"github.com/platform-mesh/golang-commons/errors"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/kontext"

	corev1alpha1 "github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
	"github.com/platform-mesh/account-operator/pkg/metrics"
)

const (
	WorkspaceSubroutineName      = "WorkspaceSubroutine"
	WorkspaceSubroutineFinalizer = "account.core.platform-mesh.io/finalizer"
	workspaceTypeRequeueDelay    = 5 * time.Second
	forbiddenRequeueDelay        = 30 * time.Second
)

type WorkspaceSubroutine struct {
	client  client.Client
	limiter workqueue.TypedRateLimiter[ClusteredName]
}

func NewWorkspaceSubroutine(client client.Client) *WorkspaceSubroutine {
	exp := workqueue.NewTypedItemExponentialFailureRateLimiter[ClusteredName](1*time.Second, 120*time.Second)
	return &WorkspaceSubroutine{client: client, limiter: exp}
}

func (r *WorkspaceSubroutine) GetName() string {
	return WorkspaceSubroutineName
}

func (r *WorkspaceSubroutine) Finalize(ctx context.Context, ro runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := ro.(*corev1alpha1.Account)
	cn := MustGetClusteredName(ctx, ro)
	// Safely load operator config (tests may omit it)
	cfgAny := commonconfig.LoadConfigFromContext(ctx)
	var cfg config.OperatorConfig
	if c, ok := cfgAny.(config.OperatorConfig); ok {
		cfg = c
	}

	// Use the same cluster context as creation
	ctxWS := ctx
	if instance.Spec.Type == corev1alpha1.AccountTypeOrg && cfg.Kcp.OrgWorkspaceCluster != "" {
		ctxWS = kontext.WithCluster(ctx, logicalcluster.Name(cfg.Kcp.OrgWorkspaceCluster))
	}

	ws := kcptenancyv1alpha.Workspace{}
	err := r.client.Get(ctxWS, client.ObjectKey{Name: instance.Name}, &ws)
	// Fallback: if not found in configured org cluster (config drift), try reconcile cluster
	if kerrors.IsNotFound(err) && instance.Spec.Type == corev1alpha1.AccountTypeOrg && cfg.Kcp.OrgWorkspaceCluster != "" {
		if err2 := r.client.Get(ctx, client.ObjectKey{Name: instance.Name}, &ws); err2 == nil {
			// Found in reconcile cluster; switch deletion context
			ctxWS = ctx
			err = nil
		}
	}
	if kerrors.IsNotFound(err) {
		// Successful observation that workspace no longer exists; reset rate limiter backoff.
		r.limiter.Forget(cn)
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	if ws.GetDeletionTimestamp() != nil {
		next := r.limiter.When(cn)
		return ctrl.Result{RequeueAfter: next}, nil
	}

	err = r.client.Delete(ctxWS, &ws)
	if err != nil {
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}

	// Requeue to check if the workspace was deleted
	next := r.limiter.When(cn)
	return ctrl.Result{RequeueAfter: next}, nil
}

func (r *WorkspaceSubroutine) Finalizers() []string { // coverage-ignore
	return []string{WorkspaceSubroutineFinalizer}
}

// waitForWorkspaceType checks if the WorkspaceType exists and is Ready.
// Returns (true, nil) when Ready, (false, nil) when found but not Ready,
// and (false, OperatorError) only on client errors.
func (r *WorkspaceSubroutine) waitForWorkspaceType(ctx context.Context, name string) (bool, errors.OperatorError) {
	log := ctrl.LoggerFrom(ctx)
	wt := &kcptenancyv1alpha.WorkspaceType{}
	key := client.ObjectKey{Name: name}

	log.Info("checking workspace type", "name", name)
	err := r.client.Get(ctx, key, wt)
	if err != nil {
		if kerrors.IsNotFound(err) {
			log.Info("workspace type not found yet", "name", name)
			return false, nil
		}
		log.Error(err, "failed to get workspace type", "name", name)
		return false, errors.NewOperatorError(err, true, false)
	}

	log.Info("workspace type found", "name", name, "conditions", wt.Status.Conditions)
	// Check if the workspace type is Ready
	for _, cond := range wt.Status.Conditions {
		if cond.Type == "Ready" && metav1.ConditionStatus(cond.Status) == metav1.ConditionTrue {
			log.Info("workspace type is ready", "name", name)
			return true, nil
		}
	}
	// Found but not Ready yet
	return false, nil
}
func (r *WorkspaceSubroutine) Process(ctx context.Context, runtimeObj runtimeobject.RuntimeObject) (ctrl.Result, errors.OperatorError) {
	instance := runtimeObj.(*corev1alpha1.Account)
	// Safely load operator config (tests may omit it)
	cfgAny := commonconfig.LoadConfigFromContext(ctx)
	var cfg config.OperatorConfig
	if c, ok := cfgAny.(config.OperatorConfig); ok {
		cfg = c
	}

	// Capture original cluster path (where custom WorkspaceTypes are created)
	origPath := cfg.Kcp.ProviderWorkspace
	if cl, ok := kontext.ClusterFrom(ctx); ok {
		origPath = cl.String()
	}
	// Select the cluster under which the workspace will be created
	ctxWS := ctx
	// For org accounts, always create in the designated org workspace cluster
	if instance.Spec.Type == corev1alpha1.AccountTypeOrg && cfg.Kcp.OrgWorkspaceCluster != "" {
		ctxWS = kontext.WithCluster(ctx, logicalcluster.Name(cfg.Kcp.OrgWorkspaceCluster))
	}

	// Determine workspace type name and path
	wtName := string(instance.Spec.Type)
	wtPath := cfg.Kcp.ProviderWorkspace
	var ctxWT context.Context
	switch instance.Spec.Type {
	case corev1alpha1.AccountTypeOrg:
		if cfg.Kcp.OrgWorkspaceCluster != "" {
			wtPath = cfg.Kcp.OrgWorkspaceCluster
		}
		wtName = GetOrgWorkspaceTypeName(instance.Name, wtPath)
		// Scope WT checks to the cluster hosting the type
		ctxWT = kontext.WithCluster(ctx, logicalcluster.Name(wtPath))
		if ready, opErr := r.waitForWorkspaceType(ctxWT, wtName); opErr != nil {
			return ctrl.Result{}, opErr
		} else if !ready {
			ctrl.LoggerFrom(ctx).Info("waiting for org workspace type to be ready", "name", wtName)
			return ctrl.Result{RequeueAfter: workspaceTypeRequeueDelay}, nil
		}
	case corev1alpha1.AccountTypeAccount:
		// Parse cluster path robustly: find the "orgs" segment anywhere and derive org-scoped account WT name
		segs := strings.Split(origPath, ":")
		orgName := ""
		foundOrgs := false
		for i := 0; i < len(segs); i++ {
			if segs[i] == "orgs" {
				foundOrgs = true
				if i+1 < len(segs) && segs[i+1] != "" {
					orgName = segs[i+1]
					wtPath = strings.Join(segs[:i+1], ":")
					wtName = GetAccWorkspaceTypeName(orgName, wtPath)
				} else {
					ctrl.LoggerFrom(ctx).Info("invalid cluster path: missing org segment after 'orgs'", "path", origPath)
					return ctrl.Result{RequeueAfter: workspaceTypeRequeueDelay}, nil
				}
				break
			}
		}
		if !foundOrgs {
			ctrl.LoggerFrom(ctx).Info("no 'orgs' segment in origPath; falling back to base account WorkspaceType", "origPath", origPath, "wtPath", wtPath, "wtName", wtName)
		}
		ctxWT = kontext.WithCluster(ctx, logicalcluster.Name(wtPath))
		if ready, opErr := r.waitForWorkspaceType(ctxWT, wtName); opErr != nil {
			return ctrl.Result{}, opErr
		} else if !ready {
			ctrl.LoggerFrom(ctx).Info("waiting for account workspace type to be ready", "name", wtName)
			return ctrl.Result{RequeueAfter: workspaceTypeRequeueDelay}, nil
		}
	}

	// Prepare the Workspace object; set type on initial create only
	createdWorkspace := &kcptenancyv1alpha.Workspace{ObjectMeta: metav1.ObjectMeta{Name: instance.Name}}
	_, err := controllerutil.CreateOrUpdate(ctxWS, r.client, createdWorkspace, func() error {
		// Only set the type on create; Workspace.spec.type is immutable after creation.
		// Detect create by empty ResourceVersion (set post-create by API server).
		if createdWorkspace.ResourceVersion == "" {
			createdWorkspace.Spec.Type = &kcptenancyv1alpha.WorkspaceTypeReference{
				Name: kcptenancyv1alpha.WorkspaceTypeName(wtName),
				Path: wtPath,
			}
		}

		wsCluster, wsOK := kontext.ClusterFrom(ctxWS)
		instanceCluster, instOK := kontext.ClusterFrom(ctx)
		if wsOK && instOK && wsCluster.String() == instanceCluster.String() {
			return controllerutil.SetOwnerReference(instance, createdWorkspace, r.client.Scheme())
		}

		return nil
	})
	if err != nil {
		// Enhanced forbidden handling: optional relaxed mode
		if kerrors.IsForbidden(err) {
			// Prefer WorkspaceType path for metric label to keep cardinality bounded
			wsClusterStr := wtPath
			wsClusterForLog := ""
			if c, ok := kontext.ClusterFrom(ctxWS); ok {
				wsClusterForLog = c.String()
			}
			log := ctrl.LoggerFrom(ctx).WithValues(
				"workspaceCluster", wsClusterForLog,
				"workspaceTypeName", wtName,
				"workspaceTypePath", wtPath,
				"account", instance.Name,
				"accountType", instance.Spec.Type,
			)
			if cfg.Kcp.RelaxForbiddenWorkspaceCreation {
				metrics.WorkspaceForbiddenCreations.WithLabelValues(wsClusterStr, string(instance.Spec.Type)).Inc()
				log.Info("workspace creation forbidden; relaxed mode enabled, will retry", "error", err.Error())
				return ctrl.Result{RequeueAfter: forbiddenRequeueDelay}, nil
			}
			log.Error(err, "workspace creation forbidden; failing in strict mode")
			return ctrl.Result{}, errors.NewOperatorError(err, true, true)
		}
		return ctrl.Result{}, errors.NewOperatorError(err, true, true)
	}
	return ctrl.Result{}, nil
}
