package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/apprenda/kismatic/pkg/install"
	"github.com/apprenda/kismatic/pkg/util"
	"github.com/spf13/cobra"
)

type applyCmd struct {
	out                io.Writer
	planner            install.Planner
	executor           install.Executor
	planFile           string
	generatedAssetsDir string
	verbose            bool
	outputFormat       string
	skipPreFlight      bool
	restartServices    bool
	limit              []string
}

type applyOpts struct {
	generatedAssetsDir string
	restartServices    bool
	verbose            bool
	outputFormat       string
	skipPreFlight      bool
	limit              []string
}

// NewCmdApply creates a cluter using the plan file
func NewCmdApply(out io.Writer, installOpts *installOpts) *cobra.Command {
	applyOpts := applyOpts{}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "apply your plan file to create a Kubernetes cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("Unexpected args: %v", args)
			}
			planner := &install.FilePlanner{File: installOpts.planFilename}
			executorOpts := install.ExecutorOptions{
				GeneratedAssetsDirectory: applyOpts.generatedAssetsDir,
				OutputFormat:             applyOpts.outputFormat,
				Verbose:                  applyOpts.verbose,
			}
			executor, err := install.NewExecutor(out, os.Stderr, executorOpts)
			if err != nil {
				return err
			}

			applyCmd := &applyCmd{
				out:                out,
				planner:            planner,
				executor:           executor,
				planFile:           installOpts.planFilename,
				generatedAssetsDir: applyOpts.generatedAssetsDir,
				verbose:            applyOpts.verbose,
				outputFormat:       applyOpts.outputFormat,
				skipPreFlight:      applyOpts.skipPreFlight,
				restartServices:    applyOpts.restartServices,
				limit:              applyOpts.limit,
			}
			return applyCmd.run()
		},
	}

	// Flags
	cmd.Flags().StringSliceVar(&applyOpts.limit, "limit", []string{}, "comma-separated list of hostnames to limit the execution to a subset of nodes")
	cmd.Flags().StringVar(&applyOpts.generatedAssetsDir, "generated-assets-dir", "generated", "path to the directory where assets generated during the installation process will be stored")
	cmd.Flags().BoolVar(&applyOpts.restartServices, "restart-services", false, "force restart cluster services (Use with care)")
	cmd.Flags().BoolVar(&applyOpts.verbose, "verbose", false, "enable verbose logging from the installation")
	cmd.Flags().StringVarP(&applyOpts.outputFormat, "output", "o", "simple", "installation output format (options \"simple\"|\"raw\")")
	cmd.Flags().BoolVar(&applyOpts.skipPreFlight, "skip-preflight", false, "skip pre-flight checks, useful when rerunning kismatic")

	return cmd
}

func (c *applyCmd) run() error {
	// Validate and run pre-flight
	opts := &validateOpts{
		planFile:           c.planFile,
		verbose:            c.verbose,
		outputFormat:       c.outputFormat,
		skipPreFlight:      c.skipPreFlight,
		generatedAssetsDir: c.generatedAssetsDir,
		limit:              c.limit,
	}
	err := doValidate(c.out, c.planner, opts)
	if err != nil {
		return fmt.Errorf("error validating plan: %v", err)
	}
	plan, err := c.planner.Read()
	if err != nil {
		return fmt.Errorf("error reading plan file: %v", err)
	}

	// Generate certificates
	if err := c.executor.GenerateCertificates(plan, false); err != nil {
		return fmt.Errorf("error installing: %v", err)
	}

	// Generate kubeconfig
	util.PrintHeader(c.out, "Generating Kubeconfig File", '=')
	err = install.GenerateKubeconfig(plan, c.generatedAssetsDir)
	if err != nil {
		return fmt.Errorf("error generating kubeconfig file: %v", err)
	}
	util.PrettyPrintOk(c.out, "Generated kubeconfig file in the %q directory", c.generatedAssetsDir)

	// Perform the installation
	if err := c.executor.Install(plan, c.restartServices, c.limit...); err != nil {
		return fmt.Errorf("error installing: %v", err)
	}

	// Run smoketest
	// Don't run
	if plan.NetworkConfigured() {
		if err := c.executor.RunSmokeTest(plan); err != nil {
			return fmt.Errorf("error running smoke test: %v", err)
		}
	}

	util.PrintColor(c.out, util.Green, "\nThe cluster was installed successfully!\n")
	fmt.Fprintln(c.out)

	msg := "- To use the generated kubeconfig file with kubectl:" +
		"\n    * use \"./kubectl --kubeconfig %s/kubeconfig\"" +
		"\n    * or copy the config file \"cp %[1]s/kubeconfig ~/.kube/config\"\n"
	util.PrintColor(c.out, util.Blue, msg, c.generatedAssetsDir)
	util.PrintColor(c.out, util.Blue, "- To view the Kubernetes dashboard: \"./kismatic dashboard\"\n")
	util.PrintColor(c.out, util.Blue, "- To SSH into a cluster node: \"./kismatic ssh etcd|master|worker|storage|$node.host\"\n")
	fmt.Fprintln(c.out)

	return nil
}
