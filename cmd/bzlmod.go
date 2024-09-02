package main

import "github.com/spf13/cobra"

type BzlmodOpts struct {
	arch string
	fc   string
	out  string
}

var bzlmodopts = BzlmodOpts{}

func NewBazelmodCmd() *cobra.Command {

	bzlmodCmd := &cobra.Command{
		Use:   "bzlmod",
		Short: "Manage bazeldnf bzlmod lock file",
		Long:  `From a set of dependencies keeps the bazeldnf bzlmod json lock file up to date`,
		RunE: func(cmd *cobra.Command, args []string) error {
			//return repo.NewRemoteInit(initopts.fc, initopts.arch, initopts.out).Init()
			return nil;
		},
	}

	bzlmodCmd.Flags().StringVarP(&bzlmodopts.arch, "arch", "a", "x86_64", "target architecture")
	bzlmodCmd.Flags().StringVar(&bzlmodopts.fc, "fc", "", "target fedora core release")
	bzlmodCmd.Flags().StringVarP(&bzlmodopts.out, "output", "o", "repo.yaml", "where to write the repository information")
	//err := bzlmodCmd.MarkFlagRequired("fc")
	//if err != nil {
	//	panic(err)
	//}
	return bzlmodCmd
}
