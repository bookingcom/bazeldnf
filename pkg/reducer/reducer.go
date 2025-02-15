package reducer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rmohr/bazeldnf/pkg/api"
	"github.com/rmohr/bazeldnf/pkg/api/bazeldnf"
	"github.com/rmohr/bazeldnf/pkg/repo"
	"github.com/sirupsen/logrus"
)

type RepoReducer struct {
	packageInfo      *packageInfo
	implicitRequires []string
	loader           ReducerPackageLoader
}

func (r *RepoReducer) Load() error {
	packageInfo, err := r.loader.Load()
	if err != nil {
		return err
	}
	r.packageInfo = packageInfo
	return nil
}

func (r *RepoReducer) PackageCount() int {
	return len(r.packages)
}

func (r *RepoReducer) Resolve(packages []string, ignoreMissing bool) (matched []string, involved []*api.Package, err error) {
	packages = append(packages, r.implicitRequires...)
	discovered := map[string]*api.Package{}
	pinned := map[string]*api.Package{}
	for _, req := range packages {
		found := false
		name := ""
		var candidates []*api.Package
		for i, p := range r.packageInfo.packages {
			if strings.HasPrefix(p.String(), req) {
				if strings.HasPrefix(req, p.Name) {
					if !found || len(p.Name) < len(name) {
						candidates = []*api.Package{&r.packageInfo.packages[i]}
						name = p.Name
						found = true
					} else if p.Name == name {
						candidates = append(candidates, &r.packageInfo.packages[i])
					}
				}
			}
		}
		if !found && !ignoreMissing {
			return nil, nil, fmt.Errorf("Package %s does not exist", req)
		}

		for i, p := range candidates {
			if selected, ok := discovered[p.String()]; !ok {
				discovered[p.String()] = candidates[i]
			} else {
				if selected.Repository.Priority > p.Repository.Priority {
					discovered[p.String()] = candidates[i]
				}
			}
		}

		if len(candidates) > 0 {
			matched = append(matched, candidates[0].Name)
		}
	}

	for _, v := range discovered {
		pinned[v.Name] = v
	}

	for {
		current := []string{}
		for k := range discovered {
			current = append(current, k)
		}
		for _, p := range current {
			for _, newFound := range r.requires(discovered[p]) {
				if _, exists := discovered[newFound.String()]; !exists {
					if _, exists := pinned[newFound.Name]; !exists {
						discovered[newFound.String()] = newFound
					} else {
						logrus.Debugf("excluding %s because of pinned dependency %s", newFound.String(), pinned[newFound.Name].String())
					}
				}
			}
		}
		if len(current) == len(discovered) {
			break
		}
	}

	required := map[string]struct{}{}
	for i, pkg := range discovered {
		for _, req := range pkg.Format.Requires.Entries {
			required[req.Name] = struct{}{}
		}
		involved = append(involved, discovered[i])
	}
	// remove all provides which are not required in the reduced set
	for i, pkg := range involved {
		provides := []api.Entry{}
		for j, prov := range pkg.Format.Provides.Entries {
			if _, exists := required[prov.Name]; exists || prov.Name == pkg.Name {
				provides = append(provides, pkg.Format.Provides.Entries[j])
			}
		}
		involved[i].Format.Provides.Entries = provides
	}

	return matched, involved, nil
}

func (r *RepoReducer) filterWithIgnoreRegex(candidates []*api.Package, ignoreRegex []string) []*api.Package {
	out := []*api.Package{}
	for _, p := range candidates {
		filter := false
		for _, rex := range ignoreRegex {
			if match, err := regexp.MatchString(rex, p.String()); err != nil {
				logrus.Errorf("failed to match package with regex '%v': %v", rex, err)
			} else if match {
				logrus.Warnf("Package %v is forcefully ignored by regex '%v'.", p.String(), rex)
				filter = true
				break
			}
		}
		if !filter {
			out = append(out, p)
		}
	}
	return out
}

func (r *RepoReducer) requires(p *api.Package) (wants []*api.Package) {
	for _, requires := range p.Format.Requires.Entries {
		if val, exists := r.packageInfo.provides[requires.Name]; exists {
			var packages []string
			for _, p := range val {
				packages = append(packages, p.Name)
			}
			logrus.Debugf("%s wants %v because of %v\n", p.Name, packages, requires)
			wants = append(wants, val...)
		} else {
			logrus.Debugf("%s requires %v which can't be satisfied\n", p.Name, requires)
		}
	}

	return wants
}

func NewRepoReducer(repos *bazeldnf.Repositories, repoFiles []string, baseSystem string, arch string, cacheHelper *repo.CacheHelper) *RepoReducer {
	implicitRequires := make([]string, 0, 1)
	if baseSystem != "" {
		implicitRequires = append(implicitRequires, baseSystem)
	}
	return &RepoReducer{
		packageInfo:      nil,
		implicitRequires: implicitRequires,
		loader: RepoLoader{
			repoFiles:     repoFiles,
			architectures: []string{"noarch", arch},
			arch:          arch,
			repos:         repos,
			cacheHelper:   cacheHelper,
		},
	}
}

func Resolve(repos *bazeldnf.Repositories, repoFiles []string, baseSystem, arch string, packages []string, ignoreMissing bool) (matched []string, involved []*api.Package, err error) {
	repoReducer := NewRepoReducer(repos, repoFiles, baseSystem, arch, repo.NewCacheHelper())
	logrus.Info("Loading packages.")
	if err := repoReducer.Load(); err != nil {
		return nil, nil, err
	}
	logrus.Info("Initial reduction of involved packages.")
	return repoReducer.Resolve(packages, ignoreMissing)
}
