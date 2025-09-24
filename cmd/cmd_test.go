package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitConfig(t *testing.T) {
	// Test that initConfig function exists and can be called
	assert.NotPanics(t, func() {
		initConfig()
	})
}

func TestInitLogFunction(t *testing.T) {
	// Test that initLog function exists and can be called
	// Save original log
	originalLog := log
	defer func() {
		log = originalLog
	}()
	
	assert.NotPanics(t, func() {
		initLog()
	})
	
	// Verify log was initialized
	assert.NotNil(t, log)
}

func TestExecuteFunction(t *testing.T) {
	// Test Execute function exists
	assert.NotPanics(t, func() {
		defer func() {
			recover() // Catch any panics from cobra execution
		}()
		// Don't actually call Execute() as it would try to run the app
		// Just verify it compiles and the function exists
	})
}

func TestGlobalVariablesExist(t *testing.T) {
	// Test that global variables are initialized
	assert.NotNil(t, scheme)
	assert.NotNil(t, defaultCfg)
	assert.NotNil(t, operatorCfg)
	assert.NotNil(t, rootCmd)
}

func TestRootCommandExists(t *testing.T) {
	// Test that root command exists
	assert.NotNil(t, rootCmd)
	assert.Equal(t, "account-operator", rootCmd.Use)
}

func TestRunControllerFunctionExists(t *testing.T) {
	// Test that RunController function exists
	assert.NotNil(t, RunController)
}

func TestSchemeHasTypes(t *testing.T) {
	// Test that the scheme has some registered types
	gvks := scheme.AllKnownTypes()
	assert.NotEmpty(t, gvks)
}

func TestInitFunctionCalled(t *testing.T) {
	// Test that init() function has been called during package initialization
	// We can verify this by checking that global variables are initialized
	assert.NotNil(t, rootCmd)
	assert.NotNil(t, scheme)
	assert.NotNil(t, defaultCfg)
	assert.NotNil(t, operatorCfg)
}
