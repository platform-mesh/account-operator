# Testing Guide: Custom WorkspaceTypes for Organizations

## Step 1: Setup KCP with WorkspaceAuthentication Feature Gate

### 1.1 Update KCP to main version
```bash
# Check current KCP version
kubectl get deployment root-kcp -n platform-mesh-system -o jsonpath='{.spec.template.spec.containers[0].image}'

# Update to main version if needed:
kubectl set image deployment/root-kcp -n platform-mesh-system kcp=ghcr.io/kcp-dev/kcp:main
kubectl rollout status deployment root-kcp -n platform-mesh-system --timeout=120s
```

### 1.2 Create Kyverno Policy for WorkspaceAuthentication
```bash
# Create policy file
cat > /tmp/kcp-workspace-auth-policy.yaml << 'EOF'
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: kcp-workspace-authentication-feature-gate
  annotations:
    policies.kyverno.io/title: KCP WorkspaceAuthentication Feature Gate
    policies.kyverno.io/category: Best Practices
    policies.kyverno.io/subject: Deployment
    policies.kyverno.io/description: >-
      Ensures the root-kcp deployment includes the WorkspaceAuthentication feature gate.
spec:
  validationFailureAction: Enforce
  background: true
  rules:
    - name: ensure-workspace-authentication-feature-gate
      match:
        any:
          - resources:
              kinds:
                - Deployment
              names:
                - root-kcp
              namespaces:
                - platform-mesh-system
      mutate:
        patchesJson6902: |-
          - op: add
            path: /spec/template/spec/containers/0/args/-
            value: --feature-gates=WorkspaceAuthentication=true
EOF

# Apply policy
kubectl apply -f /tmp/kcp-workspace-auth-policy.yaml

# Verify policy creation
kubectl get clusterpolicy kcp-workspace-authentication-feature-gate
```

### 1.3 Apply Feature Gate to KCP
```bash
# Restart KCP deployment to apply policy
kubectl rollout restart deployment root-kcp -n platform-mesh-system
kubectl rollout status deployment root-kcp -n platform-mesh-system --timeout=120s

# Verify feature gate is applied
kubectl get pod $(kubectl get pods -n platform-mesh-system | grep root-kcp | grep Running | awk '{print $1}') -n platform-mesh-system -o jsonpath='{.spec.containers[0].args}' | tr ' ' '\n' | grep feature-gates

# Expected result: --feature-gates=WorkspaceAuthentication=true
```

---

## Step 2: Build and Deploy Custom Account-Operator

### 2.1 Build Docker image
```bash
# Navigate to account-operator directory
cd /path/to/account-operator
# Or adjust path to your actual location

# Build Docker image with custom tag
docker build -t account-operator:test-custom-workspacetypes .

# Load image into kind cluster
kind load docker-image account-operator:test-custom-workspacetypes --name platform-mesh
```

### 2.2 Update deployment
```bash
# Update account-operator image
kubectl set image deployment/account-operator -n platform-mesh-system operator=account-operator:test-custom-workspacetypes

# Wait for deployment readiness
kubectl rollout status deployment account-operator -n platform-mesh-system --timeout=60s

# Verify new image is used
kubectl get deployment account-operator -n platform-mesh-system -o jsonpath='{.spec.template.spec.containers[0].image}'
```

---

## Step 3: Test Custom WorkspaceTypes

### 3.1 Create test Account
```bash
# Create test-account.yaml
cat > test-account.yaml << 'EOF'
apiVersion: core.platform-mesh.io/v1alpha1
kind: Account
metadata:
  name: test-org-auth
spec:
  type: organization
EOF

# Apply test Account
kubectl apply -f test-account.yaml

# Verify Account creation
kubectl get account test-org-auth

# Optional: Test account-type in organization context
# (requires running in a workspace under root:platform-mesh:orgs:acme)
cat > test-account-type.yaml << 'EOF'
apiVersion: core.platform-mesh.io/v1alpha1
kind: Account
metadata:
  name: test-account
spec:
  type: account
EOF
```

---

## Step 4: Fix KCP Connectivity Issues

### 4.1 Diagnose the problem
```bash
# Check account-operator logs
kubectl logs -n platform-mesh-system deployment/account-operator --tail=20

# Common error:
# "Path not resolved to a valid virtual workspace"
# "/services/apiexport/.../core.platform-mesh.io/apis/core.platform-mesh.io/v1alpha1"
```

### 4.2 Check component status
```bash
# Verify all pods are running
kubectl get pods -n platform-mesh-system | grep -E "(kcp|front|platform-mesh-operator|account-operator)"

# Check front-proxy logs
kubectl logs $(kubectl get pods -n platform-mesh-system | grep frontproxy | awk '{print $1}') -n platform-mesh-system --tail=10
```

### 4.3 Fix connectivity issue
```bash
# Step 1: Restart KCP
kubectl rollout restart deployment root-kcp -n platform-mesh-system
kubectl rollout status deployment root-kcp -n platform-mesh-system --timeout=120s

# Step 2: Restart front-proxy
kubectl rollout restart deployment frontproxy-front-proxy -n platform-mesh-system
kubectl rollout status deployment frontproxy-front-proxy -n platform-mesh-system --timeout=60s

# Step 3: Restart platform-mesh-operator
kubectl rollout restart deployment platform-mesh-operator -n platform-mesh-system
kubectl rollout status deployment platform-mesh-operator -n platform-mesh-system --timeout=60s

# Step 4: Restart account-operator
kubectl rollout restart deployment account-operator -n platform-mesh-system
kubectl rollout status deployment account-operator -n platform-mesh-system --timeout=60s
```

---

## Step 5: Verify Results

### 5.1 Check Account processing
```bash
# Delete and recreate Account to trigger processing
kubectl delete account test-org-auth
kubectl apply -f test-account.yaml

# Wait for processing
sleep 10

# Check Account status (should show status section)
kubectl get account test-org-auth -o yaml
```

### 5.2 Check account-operator logs
```bash
# Verify no connectivity errors
kubectl logs -n platform-mesh-system deployment/account-operator --tail=20

# Should not see errors like:
# "Path not resolved to a valid virtual workspace"
```

### 5.3 Expected results (when KCP is working)
When successful, account-operator creates:

1. **Custom Organization WorkspaceType**: `test-org-auth-org`
```yaml
apiVersion: tenancy.kcp.io/v1alpha1
kind: WorkspaceType
metadata:
  name: test-org-auth-org
  labels:
    account.core.platform-mesh.io/name: test-org-auth
spec:
  extend:
    with:
    - name: organization
```

2. **Workspace** for the organization (if running in organization context)

3. **For account-type accounts**: Custom Account WorkspaceType (e.g., `orgs-acme-acc` for an account in organization "acme")
```yaml
apiVersion: tenancy.kcp.io/v1alpha1
kind: WorkspaceType
metadata:
  name: orgs-acme-acc  # Format: orgs-{orgName}-acc
  labels:
    account.core.platform-mesh.io/name: account-name
spec:
  extend:
    with:
    - name: account
```

4. **WorkspaceAuthenticationConfiguration**: `test-org-auth-auth`
```yaml
apiVersion: tenancy.kcp.io/v1alpha1
kind: WorkspaceAuthenticationConfiguration
metadata:
  name: test-org-auth-auth
spec:
  oidc:
    issuerURL: "https://portal.dev.local:8443/keycloak/realms/default"
    clientID: "default"
    usernameClaim: "email"
    groupsClaim: "groups"
```

**Note**: Account WorkspaceType names follow these patterns:
- **Organization accounts**: `{accountName}-org` (e.g., `test-org-auth-org`)  
- **Account-type accounts in orgs**: `orgs-{orgName}-acc` (e.g., `orgs-acme-acc` for account in "acme" org)

---

## 🔧 Issues and solutions:
1. **KCP Version**: Updated from commit hash to main version for WorkspaceAuthentication support
2. **Feature Gate**: Used Kyverno policy to automatically add `--feature-gates=WorkspaceAuthentication=true`
3. **Connectivity Issue**: APIExport virtual workspace requires correct KCP component restart sequence

---

## Debugging Commands

### Check KCP API resources
```bash
# Port-forward for direct KCP access (if needed)
kubectl port-forward -n platform-mesh-system svc/root-kcp 8443:6443 &

# Test connectivity to front-proxy
kubectl run test-connectivity --rm -i --tty --image=busybox --restart=Never -- nslookup frontproxy-front-proxy.platform-mesh-system.svc.cluster.local
```

### Check Kyverno policy status
```bash
# Check policy status
kubectl describe clusterpolicy kcp-workspace-authentication-feature-gate

# Verify policy is active
kubectl get clusterpolicy kcp-workspace-authentication-feature-gate -o yaml | grep -A 5 -B 5 Ready
```

### Monitor account-operator
```bash
# Monitor account-operator logs
kubectl logs -n platform-mesh-system deployment/account-operator -f

# Watch all component status
watch kubectl get pods -n platform-mesh-system
```
