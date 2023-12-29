package main

import (
	"os"

	"github.com/bazelbuild/buildtools/build"
	"github.com/rmohr/bazeldnf/cmd/template"
	"github.com/rmohr/bazeldnf/pkg/api"
	"github.com/rmohr/bazeldnf/pkg/bazel"
	"github.com/rmohr/bazeldnf/pkg/reducer"
	"github.com/rmohr/bazeldnf/pkg/repo"
	"github.com/rmohr/bazeldnf/pkg/sat"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type rpmtreeOpts struct {
	lang             string
	nobest           bool
	arch             string
	baseSystem       string
	repofiles        []string
	workspace        string
	toMacro          string
	buildfile        string
	name             string
	public           bool
	forceIgnoreRegex []string
}

var rpmtreeopts = rpmtreeOpts{}

func getRepoReducer() (*reducer.RepoReducer, error) {
	repos, err := repo.LoadRepoFiles(rpmtreeopts.repofiles)
	if err != nil {
		return nil, err
	}
	repoReducer := reducer.NewRepoReducer(repos, nil, rpmtreeopts.lang, rpmtreeopts.baseSystem, rpmtreeopts.arch, ".bazeldnf")
	logrus.Info("Loading packages.")
	if err := repoReducer.Load(); err != nil {
		return nil, err
	}

	logrus.Info("Loading packages.")
	if err := repoReducer.Load(); err != nil {
		return nil, err
	}

	return repoReducer, nil
}

func resolve(repoReducer *reducer.RepoReducer, required []string) ([]*api.Package, []*api.Package, error) {
	logrus.Info("Initial reduction of involved packages.")
	matched, involved, err := repoReducer.Resolve(required)
	if err != nil {
		return nil, nil, err
	}
	solver := sat.NewResolver(rpmtreeopts.nobest)
	logrus.Info("Loading involved packages into the rpmtreer.")
	err = solver.LoadInvolvedPackages(involved, rpmtreeopts.forceIgnoreRegex)
	if err != nil {
		return nil, nil, err
	}
	logrus.Info("Adding required packages to the rpmtreer.")
	err = solver.ConstructRequirements(matched)
	if err != nil {
		return nil, nil, err
	}
	logrus.Info("Solving.")
	install, _, forceIgnored, err := solver.Resolve()
	if err != nil {
		return nil, nil, err
	}

	return install, forceIgnored, err
}

func updateWorkspace(build *build.File, install []*api.Package, resolvedRpms map[string]api.Package) (error) {
	workspace, err := bazel.LoadWorkspace(rpmtreeopts.workspace)
	if err != nil {
		return err
	}

	err = bazel.AddWorkspaceRPMs(workspace, install, rpmtreeopts.arch, &resolvedRpms)
	if err != nil {
		return err
	}

	err = bazel.WriteWorkspace(false, workspace, rpmtreeopts.workspace)
	if err != nil {
		return err
	}

	bazel.PruneWorkspaceRPMs(build, workspace)

	return nil
}

func updateMacro(build *build.File, install []*api.Package, resolvedRpms map[string]api.Package) error {
	bzl, defName, err := bazel.ParseMacro(rpmtreeopts.toMacro)
	if err != nil {
		return err
	}

	bzlfile, err := bazel.LoadBzl(bzl)
	if err != nil {
		return err
	}

	err = bazel.AddBzlfileRPMs(bzlfile, defName, install, rpmtreeopts.arch, &resolvedRpms)
	if err != nil {
		return err
	}

	bazel.PruneBzlfileRPMs(build, bzlfile, defName)

	err = bazel.WriteBzl(false, bzlfile, bzl)
	if err != nil {
		return err
	}

	return nil
}

func implementation(cmd *cobra.Command, required []string) error {
	// implementation for the rpmtree command

	writeToMacro := rpmtreeopts.toMacro != ""

	repoReducer, err := getRepoReducer()

	if err != nil {
		return err
	}

	install, forceIgnored, err := resolve(repoReducer, required)
	if err != nil {
		return err
	}

	build, err := bazel.LoadBuild(rpmtreeopts.buildfile)
	if err != nil {
		return err
	}

	bazel.AddTree(rpmtreeopts.name, build, install, rpmtreeopts.arch, rpmtreeopts.public)

	resolvedRpms, err := bazel.GetResolvedRPMsMapping(install)
	if err != nil {
		return err
	}

	logrus.Info("Writing bazel files.")
	if writeToMacro {
		updateMacro(build, install, resolvedRpms)
	} else {
		updateWorkspace(build, install, resolvedRpms)
	}

	err = bazel.WriteBuild(false, build, rpmtreeopts.buildfile)
	if err != nil {
		return err
	}

	// dump list of resolved packages
	if err := template.Render(os.Stdout, install, forceIgnored); err != nil {
		return err
	}

	return nil
}

func NewRpmTreeCmd() *cobra.Command {

	rpmtreeCmd := &cobra.Command{
		Use:   "rpmtree",
		Short: "Writes a rpmtree rule and its rpmdependencies to bazel files",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, required []string) error {
			return implementation(cmd, required)
		},
	}

	rpmtreeCmd.Flags().StringVar(&rpmtreeopts.baseSystem, "basesystem", "fedora-release-container", "base system to use (e.g. fedora-release-server, centos-stream-release, ...)")
	rpmtreeCmd.Flags().StringVarP(&rpmtreeopts.arch, "arch", "a", "x86_64", "target architecture")
	rpmtreeCmd.Flags().BoolVarP(&rpmtreeopts.nobest, "nobest", "n", false, "allow picking versions which are not the newest")
	rpmtreeCmd.Flags().BoolVarP(&rpmtreeopts.public, "public", "p", true, "if the rpmtree rule should be public")
	rpmtreeCmd.Flags().StringArrayVarP(&rpmtreeopts.repofiles, "repofile", "r", []string{"repo.yaml"}, "repository information file. Can be specified multiple times. Will be used by default if no explicit inputs are provided.")
	rpmtreeCmd.Flags().StringVarP(&rpmtreeopts.workspace, "workspace", "w", "WORKSPACE", "Bazel workspace file")
	rpmtreeCmd.Flags().StringVarP(&rpmtreeopts.toMacro, "to-macro", "", "", "Tells bazeldnf to write the RPMs to a macro in the given bzl file instead of the WORKSPACE file. The expected format is: macroFile%defName")
	rpmtreeCmd.Flags().StringVarP(&rpmtreeopts.buildfile, "buildfile", "b", "rpm/BUILD.bazel", "Build file for RPMs")
	rpmtreeCmd.Flags().StringVar(&rpmtreeopts.name, "name", "", "rpmtree rule name")
	rpmtreeCmd.Flags().StringArrayVar(&rpmtreeopts.forceIgnoreRegex, "force-ignore-with-dependencies", []string{}, "Packages matching these regex patterns will not be installed. Allows force-removing unwanted dependencies. Be careful, this can lead to hidden missing dependencies.")
	rpmtreeCmd.MarkFlagRequired("name")
	// deprecated options
	rpmtreeCmd.Flags().StringVarP(&rpmtreeopts.baseSystem, "fedora-base-system", "f", "fedora-release-container", "base system to use (e.g. fedora-release-server, centos-stream-release, ...)")
	rpmtreeCmd.Flags().MarkDeprecated("fedora-base-system", "use --basesystem instead")
	rpmtreeCmd.Flags().MarkShorthandDeprecated("fedora-base-system", "use --basesystem instead")
	rpmtreeCmd.Flags().MarkShorthandDeprecated("nobest", "use --nobest instead")
	return rpmtreeCmd
}
