package main

import (
	"strings"

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
	fedoraBaseSystem string
	repofile         string
	workspace        string
	buildfile        string
	name             string
	public           bool
}

var rpmtreeopts = rpmtreeOpts{}

func NewrpmtreeCmd() *cobra.Command {

	rpmtreeCmd := &cobra.Command{
		Use:   "rpmtree",
		Short: "Writes a rpmtree rule and its rpmdependencies to bazel files",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, required []string) error {
			repos, err := repo.LoadRepoFile(reduceopts.repofile)
			if err != nil {
				return err
			}
			repoReducer := reducer.NewRepoReducer(repos, nil, rpmtreeopts.lang, rpmtreeopts.fedoraBaseSystem, rpmtreeopts.arch, ".bazeldnf")
			logrus.Info("Loading packages.")
			if err := repoReducer.Load(); err != nil {
				return err
			}
			logrus.Info("Initial reduction of involved packages.")
			involved, err := repoReducer.Resolve(required)
			if err != nil {
				return err
			}
			solver := sat.NewResolver(rpmtreeopts.nobest)
			logrus.Info("Loading involved packages into the rpmtreer.")
			err = solver.LoadInvolvedPackages(involved)
			if err != nil {
				return err
			}
			logrus.Info("Adding required packages to the rpmtreer.")
			err = solver.ConstructRequirements(append(required, rpmtreeopts.fedoraBaseSystem))
			if err != nil {
				return err
			}
			logrus.Info("Solving.")
			install, _, err := solver.Resolve()
			if err != nil {
				return err
			}
			workspace, err := bazel.LoadWorkspace(rpmtreeopts.workspace)
			if err != nil {
				return err
			}
			build, err := bazel.LoadBuild(rpmtreeopts.buildfile)
			if err != nil {
				return err
			}
			bazel.AddRPMs(workspace, install)
			files := []string{}
			helper := repo.CacheHelper{CacheDir: ".bazeldnf"}

			logrus.Info("Calculating header and library files.")
			remaining := install
			for i, _ := range repos.Repositories {
				var found []*api.FileListPackage
				found, remaining, err = helper.CurrentFilelistsForPackages(&repos.Repositories[i], []string{rpmtreeopts.arch, "noarch"}, remaining)
				if err != nil {
					return err
				}
				for _, pkg := range found {
					for _, file := range pkg.File {
						if file.Type != "dir" {
							if strings.HasPrefix(file.Text, "/usr/include/") ||
								strings.HasPrefix(file.Text, "/usr/lib64/") ||
								strings.HasPrefix(file.Text, "/lib64/") {
								files = append(files, file.Text)
							}
						}
					}
				}
			}
			bazel.AddTree(rpmtreeopts.name, build, install, files, rpmtreeopts.public)
			bazel.PruneRPMs(build, workspace)
			logrus.Info("Writing bazel files.")
			err = bazel.WriteWorkspace(false, workspace, rpmtreeopts.workspace)
			if err != nil {
				return err
			}
			err = bazel.WriteBuild(false, build, rpmtreeopts.buildfile)
			if err != nil {
				return err
			}
			logrus.Info("Done.")

			return nil
		},
	}

	rpmtreeCmd.PersistentFlags().StringVarP(&rpmtreeopts.fedoraBaseSystem, "fedora-base-system", "f", "fedora-release-container", "fedora base system to choose from (e.g. fedora-release-server, fedora-release-container, ...)")
	rpmtreeCmd.PersistentFlags().StringVarP(&rpmtreeopts.arch, "arch", "a", "x86_64", "target fedora architecture")
	rpmtreeCmd.PersistentFlags().BoolVarP(&rpmtreeopts.nobest, "nobest", "n", false, "allow picking versions which are not the newest")
	rpmtreeCmd.PersistentFlags().BoolVarP(&rpmtreeopts.public, "public", "p", true, "if the rpmtree rule should be public")
	rpmtreeCmd.PersistentFlags().StringVarP(&rpmtreeopts.repofile, "repofile", "r", "repo.yaml", "repository information file. Will be used by default if no explicit inputs are provided.")
	rpmtreeCmd.PersistentFlags().StringVarP(&rpmtreeopts.workspace, "workspace", "w", "WORKSPACE", "Bazel workspace file")
	rpmtreeCmd.PersistentFlags().StringVarP(&rpmtreeopts.buildfile, "buildfile", "b", "rpm/BUILD.bazel", "Build file for RPMs")
	rpmtreeCmd.Flags().StringVarP(&rpmtreeopts.name, "name", "", "", "rpmtree rule name")
	rpmtreeCmd.MarkFlagRequired("name")
	return rpmtreeCmd
}