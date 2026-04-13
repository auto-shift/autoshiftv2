// autoshift-ci is a small CLI that hosts AutoShift-specific CI validators.
//
// Subcommands:
//
//	lint-labels    Validate the autoshift.io/* label contract between policy
//	               templates (consumers) and values files (declarers).
//	               Uses the real ACM hub template resolver to verify templates
//	               resolve correctly with the declared label values.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/auto-shift/autoshiftv2/tools/internal/labels"
	"github.com/auto-shift/autoshiftv2/tools/internal/resolver"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "lint-labels":
		os.Exit(cmdLintLabels(os.Args[2:]))
	case "-h", "--help", "help":
		usage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `autoshift-ci — AutoShift CI helpers

Usage:
  autoshift-ci <subcommand> [flags]

Subcommands:
  lint-labels   Validate the autoshift.io/* label contract

Run "autoshift-ci <subcommand> --help" for subcommand-specific flags.
`)
}

func cmdLintLabels(args []string) int {
	fs := flag.NewFlagSet("lint-labels", flag.ExitOnError)
	policiesDir := fs.String("policies", "policies", "path to the policies/ directory")
	valuesDir := fs.String("values", "autoshift/values", "path to the autoshift/values/ directory")
	testdataDir := fs.String("testdata", "testdata", "path to the testdata/ directory with mock Secrets/ConfigMaps for hub template lookups")
	allowlistPath := fs.String("allowlist", ".github/label-lint-allowlist.yaml", "allowlist file (empty string to disable)")
	includeProfiles := fs.Bool("include-profiles", false, "also scan non-example profile files — by default only _example*.yaml counts as the authoritative catalog")
	format := fs.String("format", "text", "output format: text | markdown")
	output := fs.String("output", "-", "output file ('-' for stdout)")
	verbose := fs.Bool("verbose", false, "text mode: include per-key details")
	showPolicies := fs.Bool("show-policies", false, "text/verbose mode: show which policies reference each key")
	strictOrphans := fs.Bool("strict-orphans", false, "fail if any orphaned keys are reported")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// 1. Extract declared labels from example files.
	declared, err := labels.ExtractDeclaredFromTree(*valuesDir, *includeProfiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading values: %v\n", err)
		return 1
	}

	// 2. Build synthetic label map and hub context.
	syntheticLabels := resolver.BuildSyntheticLabels(declared)
	ctx := resolver.HubContext{
		ManagedClusterName:   "lint-cluster",
		ManagedClusterLabels: syntheticLabels,
	}

	// 3. Extract hub config from the example file for ConfigMap generation.
	hubConfig, err := resolver.ExtractHubConfig(*valuesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not extract hub config: %v\n", err)
	}

	// 4. Create the ACM hub template resolver with fake k8s clients.
	r, err := resolver.NewResolver(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating resolver: %v\n", err)
		return 1
	}

	// 5. Run the pipeline: helm template → resolve → validate.
	consumed, results, err := resolver.RunPipeline(*policiesDir, ctx, r, declared, hubConfig, *testdataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running pipeline: %v\n", err)
		return 1
	}

	// 5. Report per-chart results.
	helmFailed, resolveWarns, resolveClean := 0, 0, 0
	for _, res := range results {
		if res.Err != nil {
			fmt.Fprintf(os.Stderr, "  FAIL  %s: %v\n", res.Policy, res.Err)
			helmFailed++
		} else if len(res.ResolveWarns) > 0 {
			for _, w := range res.ResolveWarns {
				fmt.Fprintf(os.Stderr, "  WARN  %s: %s\n", res.Policy, w)
			}
			resolveWarns++
		} else {
			resolveClean++
		}
	}
	total := len(results)
	fmt.Fprintf(os.Stderr, "\nACM hub resolution: %d clean, %d with warnings, %d helm failures (%d total)\n\n",
		resolveClean, resolveWarns, helmFailed, total)
	hasResolutionErrors := helmFailed > 0

	// 6. Load allowlist and build the contract report.
	allow, err := labels.LoadAllowlist(*allowlistPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading allowlist: %v\n", err)
		return 1
	}

	report := labels.BuildReport(consumed, declared, allow)

	w := os.Stdout
	if *output != "-" {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening %s: %v\n", *output, err)
			return 1
		}
		defer f.Close()
		w = f
	}

	switch *format {
	case "text":
		labels.WriteText(w, report, *verbose, *showPolicies)
	case "markdown", "md":
		labels.WriteMarkdown(w, report)
	default:
		fmt.Fprintf(os.Stderr, "unknown --format %q (want text or markdown)\n", *format)
		return 2
	}

	// Exit code policy:
	//   - Always fail on missing.
	//   - Fail on orphaned if --strict-orphans.
	//   - Fail if any chart had resolution errors.
	exit := 0
	if len(report.Missing) > 0 {
		exit = 1
	}
	if *strictOrphans && len(report.Orphaned) > 0 {
		exit = 1
	}
	if hasResolutionErrors {
		exit = 1
	}
	return exit
}
