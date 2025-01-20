package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"

	"slices"

	"github.com/rmohr/bazeldnf/pkg/api"
	"github.com/rmohr/bazeldnf/pkg/api/bazeldnf"
	"github.com/rmohr/bazeldnf/pkg/reducer"
	"github.com/rmohr/bazeldnf/pkg/repo"
	"github.com/rmohr/bazeldnf/pkg/sat"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type BzlmodOpts struct {
	arch             string
	nobest           bool
	out              string
	forceIgnoreRegex []string
	repoFiles        []string
}

var bzlmodopts = BzlmodOpts{}

func NewBzlmodCmd() *cobra.Command {

	bzlmodCmd := &cobra.Command{
		Use:   "bzlmod",
		Short: "Manage bazeldnf bzlmod lock file",
		Long:  `From a set of dependencies keeps the bazeldnf bzlmod json lock file up to date`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return bzlmodopts.RunE(cmd, args)
		},
	}

	bzlmodCmd.Flags().StringVarP(&bzlmodopts.arch, "arch", "a", "x86_64", "target architecture")
	bzlmodCmd.Flags().BoolVarP(&bzlmodopts.nobest, "nobest", "n", false, "allow picking versions which are not the newest")
	bzlmodCmd.Flags().StringVarP(&bzlmodopts.out, "output", "o", "bazeldnf-lock.json", "where to write the resolved dependency tree")
	bzlmodCmd.Flags().StringArrayVarP(&bzlmodopts.forceIgnoreRegex, "force-ignore-with-dependencies", "i", []string{}, "Packages matching these regex patterns will not be installed. Allows force-removing unwanted dependencies. Be careful, this can lead to hidden missing dependencies.")
	bzlmodCmd.Flags().StringArrayVarP(&bzlmodopts.repoFiles, "repofile", "r", []string{"repo.yaml"}, "repository information file. Can be specified multiple times. Will be used by default if no explicit inputs are provided.")
	bzlmodCmd.Args = cobra.MinimumNArgs(1)

	repo.AddCacheHelperFlags(bzlmodCmd)

	return bzlmodCmd
}

type ResolvedResult struct {
	Install      []*api.Package `json:"install"`
	ForceIgnored []*api.Package `json:"force_ignored"`
}

type InstalledPackage struct {
	Name         string   `json:"name"`
	Sha256       string   `json:"sha256"`
	Href         string   `json:"href"`
	Repository   string   `json:"repository"`
	Dependencies []string `json:"dependencies"`
}

func (i *InstalledPackage) setDependencies(pkgs []string) {
	i.Dependencies = make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		if pkg == i.Name {
			logrus.Infof("Ignoring self-dependency %s", pkg)
			continue
		}
		i.Dependencies = append(i.Dependencies, pkg)
	}
}

type BzlmodLockFile struct {
	CommandLineArguments []string            `json:"cli-arguments,omitempty"`
	Repositories         map[string][]string `json:"repositories"`
	Packages             []InstalledPackage  `json:"packages"`
	Targets              []string            `json:"targets,omitempty"`
	ForceIgnored         []string            `json:"ignored,omitempty"`
}

func keys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func DumpJSON(result ResolvedResult, targets []string, cmdline []string) ([]byte, error) {
	forceIgnored := make(map[string]bool)
	allPackages := make(map[string]InstalledPackage)
	providers := make(map[string]string)
	repositories := make(map[string]*bazeldnf.Repository)

	for _, forceIgnoredPackage := range result.ForceIgnored {
		forceIgnored[forceIgnoredPackage.Name] = true

		for _, entry := range forceIgnoredPackage.Format.Provides.Entries {
			providers[entry.Name] = forceIgnoredPackage.Name
		}

		for _, entry := range forceIgnoredPackage.Format.Files {
			providers[entry.Text] = forceIgnoredPackage.Name
		}
	}

	for _, installPackage := range result.Install {
		deps := make([]string, 0, len(installPackage.Format.Requires.Entries))

		for _, entry := range installPackage.Format.Requires.Entries {
			deps = append(deps, entry.Name)
		}

		for _, entry := range installPackage.Format.Provides.Entries {
			providers[entry.Name] = installPackage.Name
		}

		for _, entry := range installPackage.Format.Files {
			providers[entry.Text] = installPackage.Name
		}

		slices.Sort(deps)
		repositories[installPackage.Repository.Name] = installPackage.Repository

		allPackages[installPackage.Name] = InstalledPackage{
			Name:         installPackage.Name,
			Sha256:       installPackage.Checksum.Text,
			Href:         installPackage.Location.Href,
			Repository:   installPackage.Repository.Name,
			Dependencies: deps,
		}
	}

	alreadyInstalled := make(map[string]bool)
	packageNames := keys(allPackages)
	for _, name := range packageNames {
		requires := allPackages[name].Dependencies
		deps, err := computeDependencies(requires, providers, forceIgnored)
		if err != nil {
			return nil, err
		}
		alreadyInstalled[name] = true

		// RPMs may have circular dependencies, even depend on themselves.
		// we need to ignore such dependency
		non_cyclic_deps := make([]string, 0, len(deps))
		for _, dep := range deps {
			if alreadyInstalled[dep] {
				continue
			}
			non_cyclic_deps = append(non_cyclic_deps, dep)
		}
		entry := allPackages[name]
		entry.setDependencies(non_cyclic_deps)
		allPackages[name] = entry
	}

	sortedPackages := make([]InstalledPackage, 0, len(packageNames))
	for _, name := range packageNames {
		sortedPackages = append(sortedPackages, allPackages[name])
	}

	lockFile := BzlmodLockFile{
		CommandLineArguments: cmdline,
		ForceIgnored:         keys(forceIgnored),
		Packages:             sortedPackages,
		Repositories:         make(map[string][]string),
	}

	for mirrorName, repository := range repositories {
		lockFile.Repositories[mirrorName] = repository.Mirrors
	}

	if len(targets) > 0 {
		lockFile.Targets = targets
	}

	return json.MarshalIndent(lockFile, "", "\t")
}

func computeDependencies(requires []string, providers map[string]string, ignored map[string]bool) ([]string, error) {
	deps := make(map[string]bool)
	for _, req := range requires {
		if ignored[req] {
			logrus.Debugf("Ignoring dependency %s", req)
			continue
		}
		logrus.Debugf("Resolving dependency %s", req)
		provider, ok := providers[req]
		if !ok {
			return nil, fmt.Errorf("could not find provider for %s", req)
		}
		logrus.Debugf("Found provider %s for %s", provider, req)
		if ignored[provider] {
			logrus.Debugf("Ignoring provider %s for %s", provider, req)
			continue
		}
		deps[provider] = true
	}
	return keys(deps), nil
}

func (opts *BzlmodOpts) RunE(cmd *cobra.Command, rpms []string) error {
	repos, err := repo.LoadRepoFiles(bzlmodopts.repoFiles)
	if err != nil {
		return err
	}

	repoReducer := reducer.NewRepoReducer(repos, bzlmodopts.repoFiles, "", opts.arch, repo.NewCacheHelper())

	logrus.Info("Loading packages.")
	if err := repoReducer.Load(); err != nil {
		return err
	}

	logrus.Infof("Initial reduction to resolve dependencies for targets %v", rpms)
	matched, involved, err := repoReducer.Resolve(rpms)
	if err != nil {
		return err
	}

	solver := sat.NewResolver(opts.nobest)
	logrus.Info("Loading involved packages into the rpmtreer.")
	err = solver.LoadInvolvedPackages(involved, opts.forceIgnoreRegex)
	if err != nil {
		return err
	}

	logrus.Info("Adding required packages to the rpmtreer.")
	err = solver.ConstructRequirements(matched)
	if err != nil {
		return err
	}

	logrus.Info("Solving.")
	install, _, forceIgnored, err := solver.Resolve()
	if err != nil {
		return err
	}

	logrus.Debugf("install: %v", install)
	logrus.Debugf("forceIgnored: %v", forceIgnored)

	result := ResolvedResult{Install: install, ForceIgnored: forceIgnored}

	data, err := DumpJSON(result, rpms, os.Args[2:])

	if err != nil {
		return err
	}

	logrus.Info("Writing lock file.")

	return os.WriteFile(opts.out, data, 0644)
}
