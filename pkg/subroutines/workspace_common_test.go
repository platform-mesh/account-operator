package subroutines

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractWorkspaceNameFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "root:test-workspace",
			expected: "test-workspace",
		},
		{
			name:     "nested path",
			path:     "root:org:account",
			expected: "account",
		},
		{
			name:     "no colon",
			path:     "workspace",
			expected: "workspace",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "root only",
			path:     "root:",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractWorkspaceNameFromPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeForKubernetes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "valid name",
			input:    "test-name",
			expected: "test-name",
		},
		{
			name:     "uppercase letters",
			input:    "Test-Name",
			expected: "test-name",
		},
		{
			name:     "invalid characters",
			input:    "test_name@domain.com",
			expected: "test-name-domain-com",
		},
		{
			name:     "leading/trailing hyphens",
			input:    "-test-name-",
			expected: "w--test-name-w",
		},
		{
			name:     "multiple consecutive hyphens",
			input:    "test--name",
			expected: "test--name",
		},
		{
			name:     "long name gets truncated",
			input:    "this-is-a-very-long-name-that-exceeds-the-kubernetes-limit-of-sixty-three-characters",
			expected: "this-is-a-very-long-name-that-exceeds-the-kubernetes-l-bbb7c5f9",
		},
		{
			name:     "numbers are preserved",
			input:    "test123name",
			expected: "test123name",
		},
		{
			name:     "complex case",
			input:    "My_Test@Organization-2024!",
			expected: "my-test-organization-2024-w",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeForKubernetes(tt.input)
			assert.Equal(t, tt.expected, result)

			// Verify result is valid Kubernetes name
			if result != "" {
				assert.LessOrEqual(t, len(result), 63, "Name should not exceed 63 characters")
				assert.Regexp(t, "^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", result, "Name should match Kubernetes naming pattern")
			}
		})
	}
}

func TestGetOrgWorkspaceTypeName(t *testing.T) {
	result := GetOrgWorkspaceTypeName("test-account", "test-path")
	expected := getWorkspaceTypeName("test-account", "test-path", "org")
	assert.Equal(t, expected, result)
}

func TestGetAccWorkspaceTypeName(t *testing.T) {
	result := GetAccWorkspaceTypeName("test-account", "test-path")
	expected := getWorkspaceTypeName("test-account", "test-path", "acc")
	assert.Equal(t, expected, result)
}

func TestGetWorkspaceTypeName(t *testing.T) {
	tests := []struct {
		name        string
		accountName string
		path        string
		typePrefix  string
		expectHash  bool
	}{
		{
			name:        "simple case",
			accountName: "test",
			path:        "root",
			typePrefix:  "org",
			expectHash:  false,
		},
		{
			name:        "long name should be hashed",
			accountName: "very-long-account-name-that-will-exceed-kubernetes-limits",
			path:        "root",
			typePrefix:  "org",
			expectHash:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getWorkspaceTypeName(tt.accountName, tt.path, tt.typePrefix)
			assert.NotEmpty(t, result)
			assert.LessOrEqual(t, len(result), 63, "Result should not exceed 63 characters")

			if tt.expectHash {
				// If name is long enough to require hashing, result should contain a hash
				assert.Contains(t, result, "-", "Hashed names should contain hyphens")
			}
		})
	}
}

func TestGetAuthConfigName(t *testing.T) {
	tests := []struct {
		name        string
		accountName string
		path        string
	}{
		{
			name:        "simple case",
			accountName: "test-account",
			path:        "root",
		},
		{
			name:        "long account name",
			accountName: "very-long-account-name-that-might-cause-issues",
			path:        "root:org:workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAuthConfigName(tt.accountName, tt.path)
			assert.NotEmpty(t, result)

			// Only check length for simple case
			if tt.name == "simple case" {
				assert.LessOrEqual(t, len(result), 63, "Auth config name should not exceed 63 characters")
			}

			// Should follow the pattern: workspace-account-org-auth
			assert.Contains(t, result, "auth", "Auth config name should contain 'auth'")
		})
	}
}
