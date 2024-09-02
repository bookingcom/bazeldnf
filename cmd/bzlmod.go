package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"slices"

	"github.com/rmohr/bazeldnf/pkg/api"
	"github.com/rmohr/bazeldnf/pkg/reducer"
	"github.com/rmohr/bazeldnf/pkg/repo"
	"github.com/rmohr/bazeldnf/pkg/sat"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type BzlmodOpts struct {
	baseSystem       string
	arch             string
	nobest           bool
	out              string
	forceIgnoreRegex []string
	repoFiles        []string
	targets          []string
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

	bzlmodCmd.Flags().StringVar(&bzlmodopts.baseSystem, "basesystem", "fedora-release-container", "base system to use (e.g. fedora-release-server, centos-stream-release, ...)")
	bzlmodCmd.Flags().StringVarP(&bzlmodopts.arch, "arch", "a", "x86_64", "target architecture")
	bzlmodCmd.Flags().BoolVarP(&bzlmodopts.nobest, "nobest", "n", false, "allow picking versions which are not the newest")
	bzlmodCmd.Flags().StringVarP(&bzlmodopts.out, "output", "o", "bazeldnf-lock.json", "where to write the resolved dependency tree")
	bzlmodCmd.Flags().StringArrayVar(&bzlmodopts.forceIgnoreRegex, "force-ignore-with-dependencies", []string{}, "Packages matching these regex patterns will not be installed. Allows force-removing unwanted dependencies. Be careful, this can lead to hidden missing dependencies.")
	bzlmodCmd.Flags().StringArrayVarP(&bzlmodopts.repoFiles, "repofile", "r", []string{"repo.yaml"}, "repository information file. Can be specified multiple times. Will be used by default if no explicit inputs are provided.")
	bzlmodCmd.Flags().StringArrayVarP(&bzlmodopts.targets, "targets", "t", []string{}, "target RPMs to add to lock file")
	err := bzlmodCmd.MarkFlagRequired("targets")
	if err != nil {
		panic(err)
	}

	repo.AddCacheHelperFlags(bzlmodCmd)

	return bzlmodCmd
}

type resolvedResult struct {
	Install      []*api.Package `json:"install"`
	ForceIgnored []*api.Package `json:"force_ignored"`
}

type InstalledPackage struct {
	Name         string   `json:"name"`
	Sha256       string   `json:"sha256"`
	Urls         []string `json:"urls"`
	Dependencies []string `json:"dependencies"`
}

type BzlmodLockFile struct {
	Packages     []InstalledPackage `json:"packages"`
	Targets      []string           `json:"targets"`
	ForceIgnored []string           `json:"ignored"`
}

func keys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func computeUrls(pkg *api.Package) ([]string, error) {
	urls := make([]string, 0, len(pkg.Repository.Mirrors))
	for _, mirror := range pkg.Repository.Mirrors {
		u, err := url.Parse(mirror)
		if err != nil {
			return nil, err
		}
		u = u.JoinPath(pkg.Location.Href)
		urls = append(urls, u.String())
	}
	return urls, nil
}

func computeDependencies(requires []string, providers map[string]string, ignored map[string]bool) ([]string, error) {
	deps := make(map[string]bool)
	for _, req := range requires {
		if provider, ok := providers[req]; ok {
			deps[provider] = true
		} else if ignored[req] {
			continue
		} else {
			return nil, fmt.Errorf("could not find provider for %s", req)
		}
	}
	return keys(deps), nil
}

func (opts *BzlmodOpts) RunE(cmd *cobra.Command, args []string) error {
	repos, err := repo.LoadRepoFiles(bzlmodopts.repoFiles)
	if err != nil {
		return err
	}

	var results = make(map[string]resolvedResult)

	for _, target := range opts.targets {
		repoReducer := reducer.NewRepoReducer(repos, nil, "", opts.baseSystem, opts.arch, repo.NewCacheHelper())

		logrus.Info("Loading packages.")
		if err := repoReducer.Load(); err != nil {
			return err
		}

		logrus.Infof("Initial reduction to resolve dependencies for target %s.", target)
		matched, involved, err := repoReducer.Resolve([]string{target})
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
		results[target] = resolvedResult{
			Install:      install,
			ForceIgnored: forceIgnored,
		}
	}

	logrus.Debugf("results: %v", results)

	forceIgnored := make(map[string]bool)
	allPackages := make(map[string]InstalledPackage)
	providers := make(map[string]string)
	for _, result := range results {
		for _, forceIgnoredPackage := range result.ForceIgnored {
			forceIgnored[forceIgnoredPackage.Name] = true
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

			urls, err := computeUrls(installPackage)

			if err != nil {
				return err
			}

			allPackages[installPackage.Name] = InstalledPackage{
				Name:         installPackage.Name,
				Sha256:       installPackage.Checksum.Text,
				Urls:         urls,
				Dependencies: deps,
			}
		}
	}

	packageNames := keys(allPackages)
	for _, name := range packageNames {
		requires := allPackages[name].Dependencies
		deps, err := computeDependencies(requires, providers, forceIgnored)
		if err != nil {
			return err
		}
		pkg := allPackages[name]
		pkg.Dependencies = deps
		allPackages[name] = pkg
	}

	sortedPackages := make([]InstalledPackage, 0, len(packageNames))
	for _, name := range packageNames {
		sortedPackages = append(sortedPackages, allPackages[name])
	}

	lockFile := BzlmodLockFile{
		ForceIgnored: keys[string](forceIgnored),
		Packages:     sortedPackages,
		Targets:      opts.targets,
	}

	data, err := json.MarshalIndent(lockFile, "", "\t")

	if err != nil {
		return err
	}

	return os.WriteFile(opts.out, data, 0644)
}
