package main

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/rmohr/bazeldnf/pkg/api"
	"github.com/rmohr/bazeldnf/pkg/api/bazeldnf"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/maps"
)

func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := maps.Keys(m)
	slices.Sort(keys)
	return keys
}

func toConfig(install, forceIgnored []*api.Package, targets []string, cmdline []string) (*bazeldnf.Config, error) {
	ignored := make(map[string]bool)
	for _, forceIgnoredPackage := range forceIgnored {
		ignored[forceIgnoredPackage.Name] = true
	}

	allPackages := make(map[string]*bazeldnf.RPM)
	repositories := make(map[string][]string)
	for _, installPackage := range install {
		repositories[installPackage.Repository.Name] = installPackage.Repository.Mirrors

		deps := make([]string, 0, len(installPackage.Format.Requires.Entries))
		for _, entry := range installPackage.Format.Requires.Entries {
			deps = append(deps, entry.Name)
		}

		slices.Sort(deps)

		allPackages[installPackage.Name] = &bazeldnf.RPM{
			Name:         installPackage.Name,
			SHA256:       installPackage.Checksum.Text,
			URLs:         []string{installPackage.Location.Href},
			Repository:   installPackage.Repository.Name,
			Dependencies: deps,
		}
	}

	providers := collectProviders(forceIgnored, install)
	packageNames := sortedKeys(allPackages)
	sortedPackages := make([]*bazeldnf.RPM, 0, len(packageNames))
	for _, name := range packageNames {
		pkg := allPackages[name]
		deps, err := collectDependencies(name, pkg.Dependencies, providers, ignored)
		if err != nil {
			return nil, err
		}

		pkg.SetDependencies(deps)

		sortedPackages = append(sortedPackages, pkg)
	}

	sortedPackages = removeCyclicDependencies(targets, sortedPackages)

	lockFile := bazeldnf.Config{
		CommandLineArguments: cmdline,
		ForceIgnored:         sortedKeys(ignored),
		RPMs:                 sortedPackages,
		Repositories:         repositories,
		Targets:              targets,
	}

	return &lockFile, nil
}

func collectProviders(pkgSets ...[]*api.Package) map[string]string {
	providers := map[string]string{}
	for _, pkgSet := range pkgSets {
		for _, pkg := range pkgSet {
			for _, entry := range pkg.Format.Provides.Entries {
				providers[entry.Name] = pkg.Name
			}

			for _, entry := range pkg.Format.Files {
				providers[entry.Text] = pkg.Name
			}
		}
	}

	return providers
}

func collectDependencies(pkg string, requires []string, providers map[string]string, ignored map[string]bool) ([]string, error) {
	depSet := make(map[string]bool)
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
		depSet[provider] = true
	}

	return sortedKeys(depSet), nil
}

func removeCyclicDependencies(targets []string, allPackages []*bazeldnf.RPM) []*bazeldnf.RPM {
	allPackagesMap := make(map[string]*bazeldnf.RPM)

	for _, installPackage := range allPackages {
		allPackagesMap[installPackage.Name] = installPackage
	}

	visitedMap := make(map[string]bool)
	recursionStack := make(map[string]bool)

	for _, target := range targets {
		if _, visited := visitedMap[target]; !visited {
			removeCyclicDependenciesHelper(allPackagesMap, target, visitedMap, recursionStack)
		}
	}

	return allPackages
}

func removeCyclicDependenciesHelper(allPackages map[string]*bazeldnf.RPM, pkg string, visitedMap, recursionStack map[string]bool) bool {
	/*
	 * This is a recursive function that removes cyclic dependencies from the
	 * dependency graph in the case cycles are found
	 */
	visitedMap[pkg] = true
	recursionStack[pkg] = true

	if _, ok := allPackages[pkg]; !ok {
		return false
	}

	if allPackages[pkg].Dependencies == nil {
		return false
	}

	cleanDependencies := make([]string, 0, len(allPackages[pkg].Dependencies))

	for _, dep := range allPackages[pkg].Dependencies {
		if _, visited := visitedMap[dep]; !visited {
			if removeCyclicDependenciesHelper(allPackages, dep, visitedMap, recursionStack) {
				// ignore cycle
				logrus.Debugf("Ignoring cyclic dependency %s -> %s", pkg, dep)
				continue
			}
		} else if _, recursed := recursionStack[dep]; recursed {
			// ignore cycle
			logrus.Debugf("Ignoring cyclic dependency in recursion stack %s -> %s", pkg, dep)
			continue
		}
		cleanDependencies = append(cleanDependencies, dep)
	}

	recursionStack[pkg] = false
	allPackages[pkg].SetDependencies(cleanDependencies)

	return false
}
