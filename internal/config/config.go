package config

// OperatorConfig struct to hold the app config
type OperatorConfig struct {
	Webhooks struct {
		Enabled  bool   `mapstructure:"webhooks-enabled" default:"false"`
		CertDir  string `mapstructure:"webhooks-cert-dir" default:"certs"`
		Port     int    `mapstructure:"webhooks-port" default:"9443"`
		DenyList string `mapstructure:"webhooks-deny-list"`
	} `mapstructure:",squash"`
	Subroutines struct {
		WorkspaceType struct {
			Enabled bool `mapstructure:"subroutines-workspace-type-enabled" default:"true"`
		} `mapstructure:",squash"`
		Workspace struct {
			Enabled bool `mapstructure:"subroutines-workspace-enabled" default:"true"`
		} `mapstructure:",squash"`
		AccountInfo struct {
			Enabled bool `mapstructure:"subroutines-account-info-enabled" default:"true"`
		} `mapstructure:",squash"`
	} `mapstructure:",squash"`
	Kcp struct {
		ApiExportEndpointSliceName string `mapstructure:"kcp-api-export-endpoint-slice-name"`
		ProviderWorkspace          string `mapstructure:"kcp-provider-workspace" default:"root"`
	} `mapstructure:",squash"`
}
