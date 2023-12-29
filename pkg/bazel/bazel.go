package bazel

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bazelbuild/buildtools/build"
	"github.com/bazelbuild/buildtools/edit"
	"github.com/rmohr/bazeldnf/pkg/api"
	"github.com/sirupsen/logrus"
)

type Artifact struct {
	rule *build.Rule
}

func LoadWorkspace(path string) (*build.File, error) {
	workspaceData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WORSPACE orig: %v", err)
	}
	workspace, err := build.ParseWorkspace(path, workspaceData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse WORSPACE orig: %v", err)
	}
	return workspace, nil
}

func LoadBuild(path string) (*build.File, error) {
	buildfileData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse BUILD.bazel orig: %v", err)
	}
	buildfile, err := build.ParseBuild(path, buildfileData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse BUILD.bazel orig: %v", err)
	}
	return buildfile, nil
}

func LoadBzl(path string) (*build.File, error) {
	bzlData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse bzl orig: %v", err)
	}
	bzl, err := build.ParseBzl(path, bzlData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse bzl orig: %v", err)
	}
	return bzl, nil
}

func LoadModule(path string) (*build.File, error) {
	moduleData, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", path, err)
	}
	module, err := build.ParseModule(path, moduleData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", path, err)
	}
	return module, nil
}

func WriteBuild(dryRun bool, buildfile *build.File, path string) error {
	if dryRun {
		fmt.Println(build.FormatString(buildfile))
		return nil
	}
	return os.WriteFile(path, build.Format(buildfile), 0666)
}

func WriteWorkspace(dryRun bool, workspace *build.File, path string) error {
	if dryRun {
		fmt.Println(build.FormatString(workspace))
		return nil
	}
	return os.WriteFile(path, build.Format(workspace), 0666)
}

func WriteBzl(dryRun bool, bzl *build.File, path string) error {
	if dryRun {
		fmt.Println(build.FormatString(bzl))
		return nil
	}
	return os.WriteFile(path, build.Format(bzl), 0666)
}

// ParseMacro parses a macro expression of the form macroFile%defName and returns the bzl file and the def name.
func ParseMacro(macro string) (bzlfile, defname string, err error) {
	parts := strings.Split(macro, "%")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid macro expression: %s", macro)
	}
	return parts[0], parts[1], nil
}

func GetWorkspaceRPMs(workspace *build.File) (rpms []*RPMRule) {
	for _, rule := range workspace.Rules("rpm") {
		rpms = append(rpms, &RPMRule{rule})
	}
	return
}

func GetBzlfileRPMs(bzlfile *build.File, defName string) (rpms []*RPMRule) {
	defStmt, err := findDefStmt(bzlfile.Stmt, defName)
	if err != nil {
		return
	}

	for _, rule := range defStmtRules(bzlfile, defStmt, "rpm") {
		rpms = append(rpms, &RPMRule{rule})
	}
	return
}

func GetResolvedRPMsMapping(pkgs []*api.Package) (map[string]api.Package, error) {
	resolvedRpms := make(map[string]api.Package);
	for _, pkg := range pkgs {
		for _, provide := range pkg.Format.Provides.Entries {
			if _, ok:= resolvedRpms[provide.Name]; ok {
				if strings.Compare(resolvedRpms[provide.Name].Name, pkg.Name) != 0 {
					return nil, fmt.Errorf("duplicated provider %s found for packages %s", pkg.Name, resolvedRpms[provide.Name].Name)
				}
				continue
			}
			resolvedRpms[provide.Name] = *pkg
		}
	}

	return resolvedRpms, nil
}

func GetListOfDependencies(pkg *api.Package, resolvedRpms *map[string]api.Package) ([]api.Package) {
	if resolvedRpms == nil {
		return nil
	}

	deps := make(map[string]*api.Package)

	for _, req := range pkg.Format.Requires.Entries {
		if req.Ver == "" {
			logrus.Warnf("Ignoring require %s for %s", req.Name, pkg.Name)
			continue
		}
		_, ok := (*resolvedRpms)[req.Name]
		if !ok {
			logrus.Warnf("No provider for %s requested by %s", req.Name, pkg.Name)
			continue
		}
		dep := (*resolvedRpms)[req.Name]
		deps[dep.Name] = &dep
	}

	values := make([]api.Package, 0, len(deps))

	for _, value := range(deps) {
		values = append(values, *value)
	}

	sort.SliceStable(values, func(i, j int) bool {
		return values[i].Name < values[j].Name
	})

	return values
}

func AddWorkspaceRPMs(workspace *build.File, pkgs []*api.Package, arch string, resolvedRpms *map[string]api.Package) error {

	rpms := map[string]*RPMRule{}

	for _, rule := range workspace.Rules("rpm") {
		rpms[rule.Name()] = &RPMRule{rule}
	}

	for _, pkg := range pkgs {
		pkgName := sanitize(pkg.String() + "." + arch)
		rule := rpms[pkgName]
		if rule == nil {
			call := &build.CallExpr{X: &build.Ident{Name: "rpm"}}
			rule = &RPMRule{&build.Rule{call, ""}}
			rpms[pkgName] = rule
		}
		rule.SetName(pkgName)
		urls := rule.URLs()
		// Configure/re-configure the URLs when
		// 1) no URLs are set, or
		// 2) the checksum changed.
		if len(urls) == 0 || (rule.SHA256() != pkg.Checksum.Text) {
			err := rule.SetURLs(pkg.Repository.Mirrors, pkg.Location.Href)
			if err != nil {
				return err
			}
		}
		rule.SetSHA256(pkg.Checksum.Text)

		deps := GetListOfDependencies(pkg, resolvedRpms)
		if deps == nil {
			continue
		}

		rule.SetDependencies(deps, arch)
	}

	rules := []*RPMRule{}
	for _, rule := range rpms {
		rules = append(rules, rule)
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Name() < rules[j].Name()
	})

	workspace.DelRules("rpm", "")
	for _, rule := range rules {
		workspace.Stmt = edit.InsertAtEnd(workspace.Stmt, rule.Call)
	}

	return nil
}

func AddBzlfileRPMs(bzlfile *build.File, defName string, pkgs []*api.Package, arch string, resolvedRpms *map[string]api.Package) error {
	defStmt, err := findDefStmt(bzlfile.Stmt, defName)
	if err != nil {
		// statement not found, create it
		defStmt = &build.DefStmt{
			Name: defName,
		}
	}

	rpms := map[string]*RPMRule{}

	for _, rule := range defStmtRules(bzlfile, defStmt, "rpm") {
		rpms[rule.Name()] = &RPMRule{rule}
	}

	for _, pkg := range pkgs {
		pkgName := sanitize(pkg.String() + "." + arch)
		rule := rpms[pkgName]
		if rule == nil {
			call := &build.CallExpr{X: &build.Ident{Name: "rpm"}, ForceMultiLine: true}
			rule = &RPMRule{&build.Rule{call, ""}}
			rpms[pkgName] = rule
		}
		rule.SetName(pkgName)
		rule.SetSHA256(pkg.Checksum.Text)
		urls := rule.URLs()
		if len(urls) == 0 {
			err := rule.SetURLs(pkg.Repository.Mirrors, pkg.Location.Href)
			if err != nil {
				return err
			}
		}
		deps := GetListOfDependencies(pkg, resolvedRpms)
		if deps == nil {
			continue
		}
		rule.SetDependencies(deps, arch)
	}

	rules := []*RPMRule{}
	for _, rule := range rpms {
		rules = append(rules, rule)
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Name() < rules[j].Name()
	})

	delDefStmt(bzlfile, defStmt)
	defStmt.Body = nil
	for _, rule := range rules {
		defStmt.Body = edit.InsertAtEnd(defStmt.Body, rule.Call)
	}
	bzlfile.Stmt = edit.InsertAtEnd(bzlfile.Stmt, defStmt)

	return nil
}

func AddTar2Files(name string, rpmtree string, buildfile *build.File, files []string, public bool) {
	tar2files := map[string]*tar2Files{}
	for _, rule := range buildfile.Rules("tar2files") {
		tar2files[rule.Name()] = &tar2Files{rule}
	}
	buildfile.DelRules("tar2files", "")
	rule := tar2files[name]
	if rule == nil {
		call := &build.CallExpr{X: &build.Ident{Name: "tar2files"}}
		rule = &tar2Files{&build.Rule{call, ""}}
		tar2files[name] = rule
	}

	sort.SliceStable(files, func(i, j int) bool {
		return files[i] < files[j]
	})

	fileMap := map[string][]string{}
	for _, file := range files {
		fileMap[filepath.Dir(file)] = append(fileMap[filepath.Dir(file)], filepath.Base(file))
	}

	dirs := []string{}
	for dir, _ := range fileMap {
		dirs = append(dirs, dir)
	}
	sort.SliceStable(dirs, func(i, j int) bool {
		return dirs[i] < dirs[j]
	})
	rule.SetFiles(dirs, fileMap)
	rule.SetName(name)
	if rpmtree != "" {
		rule.SetTar(rpmtree)
	}

	if public {
		rule.SetAttr("visibility", &build.ListExpr{List: []build.Expr{&build.StringExpr{Value: "//visibility:public"}}})
	}

	rules := []*tar2Files{}
	for _, rule := range tar2files {
		rules = append(rules, rule)
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Name() < rules[j].Name()
	})

	for _, rule := range rules {
		buildfile.Stmt = edit.InsertAtEnd(buildfile.Stmt, rule.Call)
	}
}

func AddTree(name string, buildfile *build.File, pkgs []*api.Package, arch string, public bool) {
	rpmtrees := map[string]*rpmTree{}

	for _, rule := range buildfile.Rules("rpmtree") {
		rpmtrees[rule.Name()] = &rpmTree{rule}
	}
	buildfile.DelRules("rpmtree", "")

	rpms := []string{}
	for _, pkg := range pkgs {
		pkgName := sanitize(pkg.String() + "." + arch)
		rpms = append(rpms, "@"+pkgName+"//rpm")
	}
	sort.SliceStable(rpms, func(i, j int) bool {
		return rpms[i] < rpms[j]
	})

	rule := rpmtrees[name]
	if rule == nil {
		call := &build.CallExpr{X: &build.Ident{Name: "rpmtree"}}
		rule = &rpmTree{&build.Rule{call, ""}}
		rpmtrees[name] = rule
	}
	rule.SetName(name)
	rule.SetRPMs(rpms)
	if public {
		rule.SetAttr("visibility", &build.ListExpr{List: []build.Expr{&build.StringExpr{Value: "//visibility:public"}}})
	}

	rules := []*rpmTree{}
	for _, rule := range rpmtrees {
		rules = append(rules, rule)
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].Name() < rules[j].Name()
	})

	for _, rule := range rules {
		buildfile.Stmt = edit.InsertAtEnd(buildfile.Stmt, rule.Call)
	}
}

func PruneWorkspaceRPMs(buildfile *build.File, workspace *build.File) {
	referenced := map[string]struct{}{}
	for _, pkg := range buildfile.Rules("rpmtree") {
		tree := &rpmTree{pkg}
		for _, rpm := range tree.RPMs() {
			referenced[rpm] = struct{}{}
		}
	}
	rpms := workspace.Rules("rpm")
	for _, rpm := range rpms {
		if _, exists := referenced["@"+rpm.Name()+"//rpm"]; !exists {
			workspace.DelRules("rpm", rpm.Name())
		}
	}
}

func PruneBzlfileRPMs(buildfile *build.File, bzlfile *build.File, defName string) {
	defStmt, err := findDefStmt(bzlfile.Stmt, defName)
	if err != nil {
		return
	}

	referenced := map[string]struct{}{}
	for _, pkg := range buildfile.Rules("rpmtree") {
		tree := &rpmTree{pkg}
		for _, rpm := range tree.RPMs() {
			referenced[rpm] = struct{}{}
		}
	}
	rpms := defStmtRules(bzlfile, defStmt, "rpm")
	var referencedRPMs []*build.Rule
	for _, rpm := range rpms {
		if _, exists := referenced["@"+rpm.Name()+"//rpm"]; exists {
			referencedRPMs = append(referencedRPMs, rpm)
		}
	}

	delDefStmt(bzlfile, defStmt)
	defStmt.Body = nil
	for _, rpm := range referencedRPMs {
		defStmt.Body = edit.InsertAtEnd(defStmt.Body, rpm.Call)
	}
	bzlfile.Stmt = edit.InsertAtEnd(bzlfile.Stmt, defStmt)
}

func findDefStmt(stmts []build.Expr, name string) (*build.DefStmt, error) {
	for _, stmt := range stmts {
		if def, ok := stmt.(*build.DefStmt); ok {
			if def.Name == name {
				return def, nil
			}
		}
	}
	return nil, fmt.Errorf("could not find def %s", name)
}

func defStmtRules(buildfile *build.File, def *build.DefStmt, kind string) []*build.Rule {
	rules := []*build.Rule{}
	for _, stmt := range def.Body {
		call, ok := stmt.(*build.CallExpr)
		if !ok {
			continue
		}

		rule := buildfile.Rule(call)
		if rule == nil {
			continue
		}

		if kind != "" && rule.Kind() != kind {
			continue
		}

		rules = append(rules, rule)
	}
	return rules
}

func delDefStmt(buildfile *build.File, def *build.DefStmt) {
	var all []build.Expr
	for _, stmt := range buildfile.Stmt {
		if stmt == def {
			continue
		}
		all = append(all, stmt)
	}
	buildfile.Stmt = all
}

type RPMRule struct {
	*build.Rule
}

func (r *RPMRule) URLs() []string {
	if urlsAttr := r.Rule.Attr("urls"); urlsAttr != nil {
		if len(urlsAttr.(*build.ListExpr).List) > 0 {
			urls := []string{}
			for _, expr := range urlsAttr.(*build.ListExpr).List {
				urls = append(urls, expr.(*build.StringExpr).Value)
			}
			return urls
		}
	}
	return nil
}

func (r *RPMRule) Dependencies() []string {
	depsAttr := r.Rule.Attr("dependencies")
	if depsAttr == nil {
		return nil
	}

	depsList, ok := depsAttr.(*build.ListExpr)
	if !ok {
		return nil
	}

	deps := []string{}
	for _, dep := range depsList.List {
		deps = append(deps, dep.(*build.StringExpr).Value)
	}

	return deps
}

func (r *RPMRule) SetURLs(mirrors []string, href string) error {
	urlsAttr := []build.Expr{}
	for _, mirror := range mirrors {
		u, err := url.Parse(mirror)
		if err != nil {
			return err
		}
		u = u.JoinPath(href)
		urlsAttr = append(urlsAttr, &build.StringExpr{Value: u.String()})
	}
	r.Rule.SetAttr("urls", &build.ListExpr{List: urlsAttr, ForceMultiLine: true})
	return nil
}

func (r *RPMRule) SetDependencies(dependencies []api.Package, arch string) error {
	depsAttr := &build.ListExpr{
		List: make([]build.Expr, len(dependencies)),
		ForceMultiLine: true,
	}

	for i, dep := range dependencies {
		pkgName := sanitize(dep.String() + "." + arch)
		depLabel := "@" + pkgName + "//rpm:entry"
		depsAttr.List[i] = &build.StringExpr{Value: depLabel}
	}

	r.Rule.SetAttr("dependencies", depsAttr)

	return nil
}

func (r *RPMRule) SetName(name string) {
	r.Rule.SetAttr("name", &build.StringExpr{Value: name})
}

func (r *RPMRule) SetSHA256(sha256 string) {
	r.Rule.SetAttr("sha256", &build.StringExpr{Value: sha256})
}

func (r *RPMRule) SHA256() string {
	return r.Rule.AttrString("sha256")
}

type rpmTree struct {
	*build.Rule
}

type tar2Files struct {
	*build.Rule
}

func (r *rpmTree) SetName(name string) {
	r.Rule.SetAttr("name", &build.StringExpr{Value: name})
}

func (r *tar2Files) SetName(name string) {
	r.Rule.SetAttr("name", &build.StringExpr{Value: name})
}

func (r *tar2Files) SetTar(name string) {
	r.Rule.SetAttr("tar", &build.StringExpr{Value: name})
}

func (r *rpmTree) RPMs() []string {
	if rpmAttrs := r.Rule.Attr("rpms"); rpmAttrs != nil {
		if len(rpmAttrs.(*build.ListExpr).List) > 0 {
			rpms := []string{}
			for _, expr := range rpmAttrs.(*build.ListExpr).List {
				rpms = append(rpms, expr.(*build.StringExpr).Value)
			}
			return rpms
		}
	}
	return nil
}

func (r *rpmTree) SetRPMs(rpms []string) {
	rpmsAttr := []build.Expr{}
	for _, rpm := range rpms {
		rpmsAttr = append(rpmsAttr, &build.StringExpr{Value: rpm})
	}
	r.Rule.SetAttr("rpms", &build.ListExpr{List: rpmsAttr})
}

func (r *tar2Files) SetFiles(dirs []string, fileMap map[string][]string) {
	filesMapExpr := &build.DictExpr{}
	for _, dir := range dirs {
		filesListExpr := &build.ListExpr{}
		for _, file := range fileMap[dir] {
			filesListExpr.List = append(filesListExpr.List, &build.StringExpr{Value: file})
		}
		filesMapExpr.List = append(filesMapExpr.List, &build.KeyValueExpr{Key: &build.StringExpr{Value: dir}, Value: filesListExpr})
	}
	r.Rule.SetAttr("files", filesMapExpr)
}

func sanitize(name string) string {
	name = strings.ReplaceAll(name, ":", "__")
	name = strings.ReplaceAll(name, "+", "__plus__")
	name = strings.ReplaceAll(name, "~", "__tilde__")
	name = strings.ReplaceAll(name, "^", "__caret__")
	return name
}
