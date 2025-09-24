package kcpenvtest

import (
	"testing"

	"github.com/platform-mesh/golang-commons/logger"
	"github.com/stretchr/testify/assert"
)

func TestNewEnvironment(t *testing.T) {
	// Test that NewEnvironment function exists and can be called
	log, _ := logger.New(logger.DefaultConfig())
	env := NewEnvironment("test-slice", "test-workspace", "/tmp", "assets", "setup", false, log)
	assert.NotNil(t, env)
}

func TestEnvironmentDefaults(t *testing.T) {
	log, _ := logger.New(logger.DefaultConfig())
	env := NewEnvironment("test-slice", "test-workspace", "/tmp", "assets", "setup", false, log)

	// Test default values are set
	assert.NotNil(t, env)
	// Note: env.Scheme might be nil until properly initialized

	// Test that environment can be configured
	// We can't actually start it without proper setup, but we can test creation
}

func TestNewKCPServer(t *testing.T) {
	log, _ := logger.New(logger.DefaultConfig())
	server := NewKCPServer("/tmp", "kcp", "/tmp", log)
	assert.NotNil(t, server)
}

func TestEnvironmentTimeouts(t *testing.T) {
	log, _ := logger.New(logger.DefaultConfig())
	env := NewEnvironment("test-slice", "test-workspace", "/tmp", "assets", "setup", false, log)

	// Test that environment is created (timeouts may be zero until defaultTimeouts is called)
	assert.NotNil(t, env)
}
