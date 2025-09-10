package subroutines

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOrgWorkspaceTypeName(t *testing.T) {
	tests := []struct {
		name          string
		accountName   string
		workspacePath string
		expected      string
	}{
		{
			name:          "with workspace path",
			accountName:   "test-org",
			workspacePath: "root:orgs:my-workspace",
			expected:      "my-workspace-test-org-org",
		},
		{
			name:          "with empty workspace path",
			accountName:   "test-org",
			workspacePath: "",
			expected:      "test-org-org",
		},
		{
			name:          "with single component path",
			accountName:   "test-org",
			workspacePath: "root",
			expected:      "root-test-org-org",
		},
		{
			name:          "with complex workspace path",
			accountName:   "test-org",
			workspacePath: "root:orgs:parent:child",
			expected:      "child-test-org-org",
		},
		{
			name:          "with numeric workspace name",
			accountName:   "test-org",
			workspacePath: "root:orgs:123workspace",
			expected:      "w-123workspace-test-org-org",
		},
		{
			name:          "with long name requiring truncation",
			accountName:   "very-long-account-name-that-exceeds-limits",
			workspacePath: "root:orgs:very-long-workspace-name-that-also-exceeds-limits",
			expected:      "very-long-workspace-name-that-also-exceeds-limits-very-7a73f98a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetOrgWorkspaceTypeName(tt.accountName, tt.workspacePath)
			assert.Equal(t, tt.expected, result)
			assert.Regexp(t, `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`, result)
			assert.LessOrEqual(t, len(result), 63)
		})
	}
}

func TestGetAccWorkspaceTypeName(t *testing.T) {
	tests := []struct {
		name          string
		accountName   string
		workspacePath string
		expected      string
	}{
		{
			name:          "with workspace path",
			accountName:   "test-acc",
			workspacePath: "root:orgs:my-workspace",
			expected:      "my-workspace-test-acc-acc",
		},
		{
			name:          "with empty workspace path",
			accountName:   "test-acc",
			workspacePath: "",
			expected:      "test-acc-acc",
		},
		{
			name:          "with single component path",
			accountName:   "test-acc",
			workspacePath: "root",
			expected:      "root-test-acc-acc",
		},
		{
			name:          "with complex workspace path",
			accountName:   "test-acc",
			workspacePath: "root:orgs:parent:child",
			expected:      "child-test-acc-acc",
		},
		{
			name:          "with numeric workspace name",
			accountName:   "test-acc",
			workspacePath: "root:orgs:123workspace",
			expected:      "w-123workspace-test-acc-acc",
		},
		{
			name:          "with long name requiring truncation",
			accountName:   "very-long-account-name-that-exceeds-limits",
			workspacePath: "root:orgs:very-long-workspace-name-that-also-exceeds-limits",
			expected:      "very-long-workspace-name-that-also-exceeds-limits-very-78eb1d5b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAccWorkspaceTypeName(tt.accountName, tt.workspacePath)
			assert.Equal(t, tt.expected, result)
			assert.Regexp(t, `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`, result)
			assert.LessOrEqual(t, len(result), 63)
		})
	}
}

func TestExtractWorkspaceNameFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "standard path",
			path:     "root:orgs:my-workspace",
			expected: "my-workspace",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "single component",
			path:     "root",
			expected: "root",
		},
		{
			name:     "nested path",
			path:     "root:orgs:parent:child",
			expected: "child",
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
			input:    "valid-name",
			expected: "valid-name",
		},
		{
			name:     "name starting with number",
			input:    "123name",
			expected: "w-123name",
		},
		{
			name:     "name with uppercase",
			input:    "UPPERCASE-Name",
			expected: "uppercase-name",
		},
		{
			name:     "name with invalid characters",
			input:    "name@with#special$chars",
			expected: "name-with-special-chars",
		},
		{
			name:     "name ending with hyphen",
			input:    "name-",
			expected: "name-w",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "very long name",
			input:    "very-long-name-that-exceeds-the-sixty-three-character-limit-for-kubernetes-resources",
			expected: "very-long-name-that-exceeds-the-sixty-three-character--d578f605",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeForKubernetes(tt.input)
			assert.Equal(t, tt.expected, result)
			if result != "" {
				assert.Regexp(t, `^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`, result)
				assert.LessOrEqual(t, len(result), 63)
			}
		})
	}
}
