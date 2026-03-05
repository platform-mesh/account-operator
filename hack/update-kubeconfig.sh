#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SECRET_DIR="$PROJECT_ROOT/.secret/kcp"
KCP_KUBECONFIG="$PROJECT_ROOT/../helm-charts/.secret/kcp/admin.kubeconfig"
KCP_SERVER="https://localhost:8443/clusters/root:platform-mesh-system"

echo "Checking for Kind cluster 'platform-mesh'..."

if ! kind get clusters | grep -q "^platform-mesh$"; then
    echo "Error: Kind cluster 'platform-mesh' not found"
    echo "Available clusters:"
    kind get clusters
    exit 1
fi

echo ""
echo "Retrieving credentials from KCP kubeconfig..."

if [ ! -f "$KCP_KUBECONFIG" ]; then
    echo "Error: KCP kubeconfig not found at $KCP_KUBECONFIG"
    echo "Run 'task local-setup' in helm-charts/ first."
    exit 1
fi

CA_DATA=$(yq eval '.clusters[] | select(.name == "workspace.kcp.io/current") | .cluster.certificate-authority-data' "$KCP_KUBECONFIG")
CLIENT_CERT_DATA=$(yq eval '.users[] | select(.name == "kcp-admin") | .user.client-certificate-data' "$KCP_KUBECONFIG")
CLIENT_KEY_DATA=$(yq eval '.users[] | select(.name == "kcp-admin") | .user.client-key-data' "$KCP_KUBECONFIG")

if [ "$CA_DATA" == "null" ] || [ -z "$CA_DATA" ]; then
    echo "Error: Failed to extract certificate-authority-data from kubeconfig"
    exit 1
fi

if [ "$CLIENT_CERT_DATA" == "null" ] || [ -z "$CLIENT_CERT_DATA" ]; then
    echo "Error: Failed to extract client-certificate-data from kubeconfig"
    exit 1
fi

if [ "$CLIENT_KEY_DATA" == "null" ] || [ -z "$CLIENT_KEY_DATA" ]; then
    echo "Error: Failed to extract client-key-data from kubeconfig"
    exit 1
fi

mkdir -p "$SECRET_DIR"

echo "Writing certificate files..."
echo "$CA_DATA" | base64 -d > "$SECRET_DIR/ca.crt"
echo "$CLIENT_CERT_DATA" | base64 -d > "$SECRET_DIR/client.crt"
echo "$CLIENT_KEY_DATA" | base64 -d > "$SECRET_DIR/client.key"

KUBECONFIG_YAML="$SECRET_DIR/admin.kubeconfig"

echo "Writing $KUBECONFIG_YAML..."
cat > "$KUBECONFIG_YAML" <<EOF
apiVersion: v1
kind: Config
current-context: workspace.kcp.io/current
clusters:
- cluster:
    certificate-authority-data: ${CA_DATA}
    server: ${KCP_SERVER}
  name: workspace.kcp.io/current
contexts:
- context:
    cluster: workspace.kcp.io/current
    user: kcp-admin
  name: workspace.kcp.io/current
users:
- name: kcp-admin
  user:
    client-certificate-data: ${CLIENT_CERT_DATA}
    client-key-data: ${CLIENT_KEY_DATA}
EOF

echo ""
echo "Successfully configured kubeconfig at $KUBECONFIG_YAML"
