package cmd

import (
	kcptypes "github.com/platform-mesh/account-operator/pkg/types"
	platformmeshcontext "github.com/platform-mesh/golang-commons/config"
	"github.com/platform-mesh/golang-commons/logger"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/platform-mesh/account-operator/api/v1alpha1"
	"github.com/platform-mesh/account-operator/internal/config"
)

var (
	scheme      = runtime.NewScheme()
	operatorCfg config.OperatorConfig
	defaultCfg  *platformmeshcontext.CommonServiceConfig
	v           *viper.Viper
	log         *logger.Logger
)

var rootCmd = &cobra.Command{
	Use:   "account-operator",
	Short: "operator to reconcile Accounts",
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	utilruntime.Must(kcptypes.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	rootCmd.AddCommand(operatorCmd)

	var err error
	v, defaultCfg, err = platformmeshcontext.NewDefaultConfig(rootCmd)
	if err != nil {
		panic(err)
	}

	cobra.OnInitialize(initConfig)

	err = platformmeshcontext.BindConfigToFlags(v, operatorCmd, &operatorCfg)
	if err != nil {
		panic(err)
	}

	cobra.OnInitialize(initLog)
}

func initConfig() {

}

func initLog() { // coverage-ignore
	logcfg := logger.DefaultConfig()
	logcfg.Level = defaultCfg.Log.Level
	logcfg.NoJSON = defaultCfg.Log.NoJson

	var err error
	log, err = logger.New(logcfg)
	if err != nil {
		panic(err)
	}
	ctrl.SetLogger(log.Logr())
}

func Execute() { // coverage-ignore
	cobra.CheckErr(rootCmd.Execute())
}
