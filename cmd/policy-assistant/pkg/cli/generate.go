package cli

import (
	"fmt"
	"strings"
	"sync"

	"github.com/mattfenwick/collections/pkg/json"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"sigs.k8s.io/network-policy-api/policy-assistant/pkg/connectivity"
	"sigs.k8s.io/network-policy-api/policy-assistant/pkg/connectivity/probe"
	"sigs.k8s.io/network-policy-api/policy-assistant/pkg/generator"
	"sigs.k8s.io/network-policy-api/policy-assistant/pkg/kube"
	"sigs.k8s.io/network-policy-api/policy-assistant/pkg/utils"
)

var (
	DefaultExcludeTags = []string{
		generator.TagMultiPeer,
		generator.TagUpstreamE2E,
		generator.TagExample,
		generator.TagEndPort,
		generator.TagNamespacesByDefaultLabel}
)

type GenerateArgs struct {
	AllowDNS                  bool
	Noisy                     bool
	IgnoreLoopback            bool
	PerturbationWaitSeconds   int
	PodCreationTimeoutSeconds int
	Retries                   int
	ParallelWorkers           int
	Context                   string
	ServerPorts               []int
	ServerProtocols           []string
	ServerNamespaces          []string
	ServerPods                []string
	CleanupNamespaces         bool
	FailFast                  bool
	Include                   []string
	Exclude                   []string
	DestinationType           string
	Mock                      bool
	DryRun                    bool
	JobTimeoutSeconds         int
	JunitResultsFile          string
	ImageRegistry             string
	//BatchJobs                 bool
}

func SetupGenerateCommand() *cobra.Command {
	args := &GenerateArgs{}

	command := &cobra.Command{
		Use:   "generate",
		Short: "generate network policies",
		Long:  "generate network policies, create and probe against kubernetes, and compare to expected results",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, as []string) {
			RunGenerateCommand(args)
		},
	}

	command.Flags().StringSliceVar(&args.ServerProtocols, "server-protocol", []string{"TCP", "UDP", "SCTP"}, "protocols to run server on")
	command.Flags().IntSliceVar(&args.ServerPorts, "server-port", []int{80, 81}, "ports to run server on")
	command.Flags().StringSliceVar(&args.ServerNamespaces, "namespace", []string{"x", "y", "z"}, "namespaces to create/use pods in")
	command.Flags().StringSliceVar(&args.ServerPods, "pod", []string{"a", "b", "c"}, "pods to create in namespaces")

	//command.Flags().BoolVar(&args.BatchJobs, "batch-jobs", false, "if true, run jobs in batches to avoid saturating the Kube APIServer with too many exec requests")
	command.Flags().IntVar(&args.Retries, "retries", 1, "number of kube probe retries to allow, if probe fails")
	command.Flags().BoolVar(&args.AllowDNS, "allow-dns", true, "if using egress, allow tcp and udp over port 53 for DNS resolution")
	command.Flags().BoolVar(&args.Noisy, "noisy", false, "if true, print all results")
	command.Flags().BoolVar(&args.IgnoreLoopback, "ignore-loopback", false, "if true, ignore loopback for truthtable correctness verification")
	command.Flags().IntVar(&args.PerturbationWaitSeconds, "perturbation-wait-seconds", 5, "number of seconds to wait after perturbing the cluster (i.e. create a network policy, modify a ns/pod label) before running probes, to give the CNI time to update the cluster state")
	command.Flags().IntVar(&args.PodCreationTimeoutSeconds, "pod-creation-timeout-seconds", 60, "number of seconds to wait for pods to create, be running and have IP addresses")
	command.Flags().StringVar(&args.Context, "context", "", "kubernetes context to use; if empty, uses default context")
	command.Flags().BoolVar(&args.CleanupNamespaces, "cleanup-namespaces", false, "if true, clean up namespaces after completion")
	command.Flags().BoolVar(&args.FailFast, "fail-fast", false, "if true, stop running tests after the first failure")
	command.Flags().StringVar(&args.DestinationType, "destination-type", "", "override to set what to direct requests at; if not specified, the tests will be left as-is; one of "+strings.Join(generator.AllProbeModes, ", "))
	command.Flags().IntVar(&args.JobTimeoutSeconds, "job-timeout-seconds", 10, "number of seconds to pass on to 'agnhost connect --timeout=%ds' flag")

	command.Flags().StringSliceVar(&args.Include, "include", []string{}, "include tests with any of these tags; if empty, all tests will be included.  Valid tags:\n"+strings.Join(generator.TagSlice, "\n"))
	command.Flags().StringSliceVar(&args.Exclude, "exclude", DefaultExcludeTags, "exclude tests with any of these tags.  See 'include' field for valid tags")

	command.Flags().BoolVar(&args.Mock, "mock", false, "if true, use a mock kube runner (i.e. don't actually run tests against kubernetes; instead, product fake results")
	command.Flags().BoolVar(&args.DryRun, "dry-run", false, "if true, don't actually do anything: just print out what would be done")

	command.Flags().StringVar(&args.JunitResultsFile, "junit-results-file", "", "output junit results to the specified file")
	command.Flags().StringVar(&args.ImageRegistry, "image-registry", "registry.k8s.io", "Image registry for agnhost")

	command.Flags().IntVar(&args.ParallelWorkers, "parallel-workers", 1, "number of parallel test workers; each worker gets its own set of namespaces and pods")

	return command
}

// workerNamespaceSuffix returns the namespace suffix for a given worker ID.
// Worker 0 gets no suffix (uses original namespace names).
func workerNamespaceSuffix(workerID int) string {
	if workerID == 0 {
		return ""
	}
	return fmt.Sprintf("-w%d", workerID)
}

// workerNamespaces returns the namespace names for a given worker.
func workerNamespaces(workerID int, baseNamespaces []string) []string {
	suffix := workerNamespaceSuffix(workerID)
	result := make([]string, len(baseNamespaces))
	for i, ns := range baseNamespaces {
		result[i] = ns + suffix
	}
	return result
}

// workerNamespaceLabels builds a labels map for a worker's namespaces.
// Each worker namespace gets the same labels as the corresponding base namespace
// (e.g., x-w1 gets {"ns": "x"}) so that policy namespace selectors match correctly.
func workerNamespaceLabels(workerID int, baseNamespaces []string) map[string]map[string]string {
	if workerID == 0 {
		return nil // use default labels
	}
	suffix := workerNamespaceSuffix(workerID)
	labels := make(map[string]map[string]string, len(baseNamespaces))
	for _, ns := range baseNamespaces {
		labels[ns+suffix] = map[string]string{"ns": ns}
	}
	return labels
}

type indexedResult struct {
	Index  int
	Result *connectivity.Result
}

func RunGenerateCommand(args *GenerateArgs) {
	fmt.Printf("args: \n%s\n", json.MustMarshalToString(args))

	RunVersionCommand()

	utils.DoOrDie(generator.ValidateTags(append(args.Include, args.Exclude...)))

	externalIPs := []string{} // "http://www.google.com"} // TODO make these be IPs?  or not?

	var kubernetes kube.IKubernetes
	if args.Mock {
		kubernetes = kube.NewMockKubernetes(1.0)
	} else {
		kubeClient, err := kube.NewKubernetesForContext(args.Context)
		utils.DoOrDie(err)
		info, err := kubeClient.ClientSet.ServerVersion()
		utils.DoOrDie(err)
		fmt.Printf("Kubernetes server version: \n%s\n", json.MustMarshalToString(info))
		kubernetes = kubeClient
	}

	serverProtocols := parseProtocols(args.ServerProtocols)

	batchJobs := false // args.BatchJobs

	numWorkers := args.ParallelWorkers
	if numWorkers < 1 {
		numWorkers = 1
	}

	// Create resources for all workers (in parallel to speed up pod scheduling)
	allResources := make([]*probe.Resources, numWorkers)
	if numWorkers == 1 {
		resources, err := probe.NewDefaultResources(kubernetes, args.ServerNamespaces, args.ServerPods, args.ServerPorts, serverProtocols, externalIPs, args.PodCreationTimeoutSeconds, batchJobs, args.ImageRegistry)
		utils.DoOrDie(err)
		allResources[0] = resources
	} else {
		var mu sync.Mutex
		var firstErr error
		var wg sync.WaitGroup
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				wns := workerNamespaces(workerID, args.ServerNamespaces)
				nsLabels := workerNamespaceLabels(workerID, args.ServerNamespaces)
				res, err := probe.NewDefaultResourcesWithLabels(kubernetes, wns, args.ServerPods, args.ServerPorts, serverProtocols, externalIPs, args.PodCreationTimeoutSeconds, batchJobs, args.ImageRegistry, nsLabels)
				mu.Lock()
				defer mu.Unlock()
				if err != nil && firstErr == nil {
					firstErr = err
				}
				allResources[workerID] = res
			}(w)
		}
		wg.Wait()
		utils.DoOrDie(firstErr)
	}

	// Use worker 0's resources (original namespace names) for test case generation
	zcPod, err := allResources[0].GetPod(args.ServerNamespaces[len(args.ServerNamespaces)-1], args.ServerPods[len(args.ServerPods)-1])
	utils.DoOrDie(err)

	testCaseGenerator := generator.NewTestCaseGenerator(args.AllowDNS, zcPod.IP, args.ServerNamespaces, args.Include, args.Exclude)

	testCases := testCaseGenerator.GenerateTestCases()
	fmt.Printf("test cases to run by tag:\n")
	for tag, count := range generator.CountTestCasesByTag(testCases) {
		fmt.Printf("- %s: %d\n", tag, count)
	}
	fmt.Printf("testing %d cases with %d parallel workers\n\n", len(testCases), numWorkers)
	for i, testCase := range testCases {
		fmt.Printf("test #%d: %s\n - tags: %+v\n", i+1, testCase.Description, strings.Join(testCase.Tags.Keys(), ", "))
	}

	if args.DryRun {
		return
	}

	if args.DestinationType != "" {
		mode, err := generator.ParseProbeMode(args.DestinationType)
		utils.DoOrDie(err)
		for _, testCase := range testCases {
			for _, step := range testCase.Steps {
				step.Probe.Mode = mode
			}
		}
	}

	interpreterConfig := &connectivity.InterpreterConfig{
		ResetClusterBeforeTestCase:       true,
		KubeProbeRetries:                 args.Retries,
		PerturbationWaitSeconds:          args.PerturbationWaitSeconds,
		VerifyClusterStateBeforeTestCase: true,
		BatchJobs:                        batchJobs,
		IgnoreLoopback:                   args.IgnoreLoopback,
		JobTimeoutSeconds:                args.JobTimeoutSeconds,
		FailFast:                         args.FailFast,
	}

	// Create an interpreter per worker
	interpreters := make([]*connectivity.Interpreter, numWorkers)
	for w := 0; w < numWorkers; w++ {
		interpreters[w] = connectivity.NewInterpreter(kubernetes, allResources[w], interpreterConfig)
	}

	printer := &connectivity.Printer{
		Noisy:            args.Noisy,
		IgnoreLoopback:   args.IgnoreLoopback,
		JunitResultsFile: args.JunitResultsFile,
	}

	// Distribute test cases to workers via a channel
	type indexedTestCase struct {
		Index    int
		TestCase *generator.TestCase
	}

	testCaseChan := make(chan indexedTestCase, len(testCases))
	for i, tc := range testCases {
		testCaseChan <- indexedTestCase{Index: i, TestCase: tc}
	}
	close(testCaseChan)

	resultChan := make(chan indexedResult, len(testCases))

	var failFastOnce sync.Once
	failFastChan := make(chan struct{})

	var workerWg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		workerWg.Add(1)
		go func(workerID int) {
			defer workerWg.Done()
			suffix := workerNamespaceSuffix(workerID)
			interp := interpreters[workerID]

			for itc := range testCaseChan {
				// Check if fail-fast has been triggered
				select {
				case <-failFastChan:
					return
				default:
				}

				logrus.Infof("worker %d: starting test case #%d: %s", workerID, itc.Index+1, itc.TestCase.Description)

				remapped := itc.TestCase.RemapNamespaces(suffix)
				result := interp.ExecuteTestCase(remapped)
				// Store original test case for display (description/tags don't have namespace names)
				result.TestCase = itc.TestCase

				resultChan <- indexedResult{
					Index:  itc.Index,
					Result: result,
				}
			}
		}(w)
	}

	// Close results channel when all workers finish
	go func() {
		workerWg.Wait()
		close(resultChan)
	}()

	// Collect results from workers
	orderedResults := make([]*connectivity.Result, len(testCases))
	for ir := range resultChan {
		orderedResults[ir.Index] = ir.Result

		if args.FailFast && !ir.Result.Passed(interpreterConfig.IgnoreLoopback) {
			logrus.Warn("failing fast due to failure")
			failFastOnce.Do(func() { close(failFastChan) })
		}
	}

	// Print results in test-case index order
	for i, result := range orderedResults {
		if result == nil {
			continue // skipped due to fail-fast
		}
		fmt.Printf("completed test case #%d\n", i+1)

		utils.DoOrDie(result.Err)

		printer.PrintTestCaseResult(result)
		fmt.Printf("finished test case #%d\n\n", i+1)
	}

	printer.PrintSummary()

	if args.CleanupNamespaces {
		for w := 0; w < numWorkers; w++ {
			for _, ns := range workerNamespaces(w, args.ServerNamespaces) {
				logrus.Infof("cleaning up namespace %s", ns)
				err = kubernetes.DeleteNamespace(ns)
				if err != nil {
					logrus.Warnf("%+v", err)
				}
			}
		}
	}
}
