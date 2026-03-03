package config

import "github.com/spf13/pflag"

// OperatorConfig struct to hold the app config
type OperatorConfig struct {
	Webhooks struct {
		Enabled                bool     `mapstructure:"webhooks-enabled" default:"false"`
		CertDir                string   `mapstructure:"webhooks-cert-dir" default:"certs"`
		Port                   int      `mapstructure:"webhooks-port" default:"9443"`
		DenyList               string   `mapstructure:"webhooks-deny-list"`
		AdditionalAccountTypes []string `mapstructure:"webhooks-additional-account-types"`
	} `mapstructure:",squash"`
	Subroutines struct {
		WorkspaceType struct {
			Enabled bool `mapstructure:"subroutines-workspace-type-enabled" default:"true"`
		} `mapstructure:",squash"`
		Workspace struct {
			Enabled bool `mapstructure:"subroutines-workspace-enabled" default:"true"`
		} `mapstructure:",squash"`
		WorkspaceReady struct {
			Enabled bool `mapstructure:"subroutines-workspace-ready-enabled" default:"true"`
		} `mapstructure:",squash"`
		AccountInfo struct {
			Enabled bool `mapstructure:"subroutines-account-info-enabled" default:"true"`
		} `mapstructure:",squash"`
	} `mapstructure:",squash"`
	Controllers struct {
		AccountInfo struct {
			Enabled bool `mapstructure:"controllers-account-info-enabled" default:"true"`
		} `mapstructure:",squash"`
	} `mapstructure:",squash"`
	Kcp struct {
		ApiExportEndpointSliceName string `mapstructure:"kcp-api-export-endpoint-slice-name" default:"core.platform-mesh.io"`
		ProviderWorkspace          string `mapstructure:"kcp-provider-workspace" default:"root"`
	} `mapstructure:",squash"`
}

func NewOperatorConfig() OperatorConfig {
	cfg := OperatorConfig{}
	cfg.Webhooks.CertDir = "certs"
	cfg.Webhooks.Port = 9443
	cfg.Subroutines.WorkspaceType.Enabled = true
	cfg.Subroutines.Workspace.Enabled = true
	cfg.Subroutines.WorkspaceReady.Enabled = true
	cfg.Subroutines.AccountInfo.Enabled = true
	cfg.Controllers.AccountInfo.Enabled = true
	cfg.Kcp.ApiExportEndpointSliceName = "core.platform-mesh.io"
	cfg.Kcp.ProviderWorkspace = "root"
	return cfg
}

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&c.Webhooks.Enabled, "webhooks-enabled", c.Webhooks.Enabled, "Enable webhook server")
	fs.StringVar(&c.Webhooks.CertDir, "webhooks-cert-dir", c.Webhooks.CertDir, "Set webhook certificate directory")
	fs.IntVar(&c.Webhooks.Port, "webhooks-port", c.Webhooks.Port, "Set webhook server port")
	fs.StringVar(&c.Webhooks.DenyList, "webhooks-deny-list", c.Webhooks.DenyList, "Comma-separated list of denied account names")
	fs.StringSliceVar(&c.Webhooks.AdditionalAccountTypes, "webhooks-additional-account-types", c.Webhooks.AdditionalAccountTypes, "Additional allowed account types")

	fs.BoolVar(&c.Subroutines.WorkspaceType.Enabled, "subroutines-workspace-type-enabled", c.Subroutines.WorkspaceType.Enabled, "Enable workspace type subroutine")
	fs.BoolVar(&c.Subroutines.Workspace.Enabled, "subroutines-workspace-enabled", c.Subroutines.Workspace.Enabled, "Enable workspace subroutine")
	fs.BoolVar(&c.Subroutines.WorkspaceReady.Enabled, "subroutines-workspace-ready-enabled", c.Subroutines.WorkspaceReady.Enabled, "Enable workspace ready subroutine")
	fs.BoolVar(&c.Subroutines.AccountInfo.Enabled, "subroutines-account-info-enabled", c.Subroutines.AccountInfo.Enabled, "Enable account info subroutine")

	fs.BoolVar(&c.Controllers.AccountInfo.Enabled, "controllers-account-info-enabled", c.Controllers.AccountInfo.Enabled, "Enable account info controller")

	fs.StringVar(&c.Kcp.ApiExportEndpointSliceName, "kcp-api-export-endpoint-slice-name", c.Kcp.ApiExportEndpointSliceName, "Set APIExportEndpointSlice name")
	fs.StringVar(&c.Kcp.ProviderWorkspace, "kcp-provider-workspace", c.Kcp.ProviderWorkspace, "Set provider workspace")
}
