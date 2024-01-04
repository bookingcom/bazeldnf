package bazel

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"

	"github.com/bazelbuild/buildtools/build"
	"github.com/bazelbuild/buildtools/edit/bzlmod"
	"github.com/rmohr/bazeldnf/pkg/api"
)

func LoadModule(path string) (*build.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s: %v", path, err)
	}
	buildfile, err := build.ParseModule(path, data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %v", path, err)
	}
	return buildfile, nil
}

const BAZELDNF_EXTENSIONS = "@bazeldnf//:extensions.bzl"

func GetRpmDepsProxy(module *build.File) ([]string) {
	proxy := bzlmod.Proxies(module, BAZELDNF_EXTENSIONS, "rpm_deps", false)
	if (proxy == nil || len(proxy) == 0) {
		proxy = bzlmod.Proxies(module, BAZELDNF_EXTENSIONS, "rpm_deps", true)
		if (proxy == nil || len(proxy) == 0) {
			fmt.Fprintln(
				os.Stderr,
				"ERROR: Your MODULE.bazel is missing a line like `rpm_deps = use_extension(\"@bazeldnf//:extensions.bzl\", \"rpm_deps\")`")
			os.Exit(1)
		}
	}

	return proxy
}

type LockFileArgs struct {
	RpmTreeName string;
	Path string;
	Bazeldnf string;
	GeneratedVisibility []string;
	ForceIgnoreWithDependencies []string;
}

func getStringValue(arg *build.AssignExpr) (string, bool) {
	val, ok := arg.RHS.(*build.StringExpr)
	if !ok { return "", false }
	return val.Value, true
}

func getListOfStringValue(arg *build.AssignExpr) ([]string, bool) {
	l, ok := arg.RHS.(*build.ListExpr)
	if !ok { return nil, false}
	out := []string{}
	for _, v := range l.List {
		c, ok := v.(*build.StringExpr)
		if !ok { continue }
		out = append(out, c.Value)
	}
	return out, false
}

func isLockFileInstance(stmt build.Expr) (*build.CallExpr) {
	def, ok := stmt.(*build.CallExpr)
	if !ok {
		return nil
	}

	x, ok := def.X.(*build.DotExpr)
	if !ok {
		return nil
	}

	if x.Name != "lock_file" {
		return nil
	}

	k, ok := x.X.(*build.Ident);
	if !ok {
		return nil
	}

	if k.Name != "rpm_deps" {
		return nil
	}
	return def
}

func extractLockFileInstanceArgument(expr build.Expr, args LockFileArgs) (LockFileArgs){
	arg, ok := expr.(*build.AssignExpr)
	if !ok { return args }

	ident, ok := arg.LHS.(*build.Ident)
	if !ok { return args }

	switch ident.Name {

	case "rpm_tree_name":
		if val, ok := getStringValue(arg); ok {
			args.RpmTreeName = val
		}

	case "path":
		if val, ok := getStringValue(arg); ok {
			args.Path = val
		}

	case "generated_visibility":
		if val, ok := getListOfStringValue(arg); ok {
			args.GeneratedVisibility = val
		}

	case "bazeldnf":
		if val, ok := getStringValue(arg); ok {
			args.Bazeldnf = val
		}
	}

	return args
}

func extractLockFileInstance(stmt build.Expr) (*LockFileArgs) {
	def := isLockFileInstance(stmt)

	if def == nil {
		return nil
	}

	out := LockFileArgs{}

	for _, l := range def.List {
		out = extractLockFileInstanceArgument(l, out)
	}

	if out.RpmTreeName == "" || out.Path == "" {
		return nil
	}

	return &out
}

func GetLockFileInstances(module *build.File) ([]LockFileArgs) {
	out := []LockFileArgs{}
	for _, stmt := range module.Stmt {
		entry := extractLockFileInstance(stmt)
		if entry == nil {
			continue
		}
		out = append(out, *entry)
	}
	return out
}

func isUseExtension(stmt build.Expr) (proxy string, bzlFile string, name string) {
	// pretty much copied from buildozer code
	assign, ok := stmt.(*build.AssignExpr)
	if !ok {
		return
	}
	if _, ok = assign.LHS.(*build.Ident); !ok {
		return
	}
	if _, ok = assign.RHS.(*build.CallExpr); !ok {
		return
	}
	call := assign.RHS.(*build.CallExpr)
	if call.X.(*build.Ident).Name != "use_extension" {
		return
	}
	if len(call.List) < 2 {
		// Missing required positional arguments.
		return
	}
	bzlFileExpr, ok := call.List[0].(*build.StringExpr)
	if !ok {
		return
	}
	nameExpr, ok := call.List[1].(*build.StringExpr)
	if !ok {
		return
	}
	return assign.LHS.(*build.Ident).Name, bzlFileExpr.Value, nameExpr.Value
}

func removeLockFileInstances(module *build.File) (int, string, *build.File, error) {
	indexToRemove := []int{}
	proxyName := ""
	useExtensionIndex := -1

	for i, stmt := range module.Stmt {
		def := isLockFileInstance(stmt)
		if def == nil {
			continue
		}
		indexToRemove = append(indexToRemove, i)
	}

	stmt := module.Stmt

	slices.Reverse(indexToRemove)

	for _, i := range(indexToRemove) {
		stmt = append(stmt[:i], stmt[i+1:]...)
	}
	module.Stmt = stmt

	for i, stmt := range module.Stmt {
		proxy, bzlFile, name := isUseExtension(stmt)
		if bzlFile != "@bazeldnf//:extensions.bzl" && name != "rpm_deps" {
			continue
		}
		useExtensionIndex = i
		proxyName = proxy
		break
	}

	if useExtensionIndex == -1 {
		return 0, "", nil, fmt.Errorf("Couldn't find use_extension call")
	}

	return useExtensionIndex, proxyName, module, nil
}

func generateLockFileStatements(proxy string, lockFiles []LockFileArgs) ([]build.Expr) {
	sort.Slice(lockFiles, func(i int, j int) bool {
		return lockFiles[i].RpmTreeName < lockFiles[j].RpmTreeName
	})

	out := []build.Expr{}

	for _, lockFile := range lockFiles {
		args := []build.Expr {
			&build.AssignExpr{
				LHS: &build.Ident{
					Name: "rpm_tree_name",
				},
				RHS: &build.StringExpr{
					Value: lockFile.RpmTreeName,
				},
				Op: "=",
			},
			&build.AssignExpr{
				LHS: &build.Ident{
					Name: "path",
				},
				RHS: &build.StringExpr{
					Value: lockFile.Path,
				},
				Op: "=",
			},
		}
		if lockFile.GeneratedVisibility != nil && len(lockFile.GeneratedVisibility) > 0 {
			visibilities := []build.Expr{}
			for _, visibility := range lockFile.GeneratedVisibility {
				visibilities = append(visibilities, &build.StringExpr {
					Value: visibility,
				})
			}
			args = append(args, &build.AssignExpr{
				LHS: &build.Ident{ Name: "generated_visibility" },
				RHS: &build.ListExpr{
					List: visibilities,
					ForceMultiLine: true,
				},
				Op: "=",
			})
		}
		line := &build.CallExpr{
			X: &build.DotExpr{
				Name: "lock_file",
				X: &build.Ident{
					Name: proxy,
				},
			},
			List: args,
			ForceMultiLine: true,
			ForceCompact: false,
		}
		out = append(out, line)
	}

	return out
}

func WriteModule(dryRun bool, module *build.File, lockFiles []LockFileArgs, moduleFilePath string, moduleName string) error {
	proxyStmtIndex, proxyName, module, err := removeLockFileInstances(module)

	if err != nil {
		return err
	}

	lockFileStatements := generateLockFileStatements(proxyName, lockFiles)

	module.Stmt = append(
		module.Stmt[:proxyStmtIndex+1],
		append(
			lockFileStatements,
			module.Stmt[proxyStmtIndex+1:]...,
		)...,
	)

	proxies := GetRpmDepsProxy(module)

	module = addRepoUsage(module, proxies, moduleName)

	if dryRun {
		fmt.Println(build.FormatString(module))
		return nil
	}

	return os.WriteFile(moduleFilePath, build.Format(module), 0644)
}

type BzlModLockFileRPM struct {
	Name string      `json:"name"`
	Sha256 string    `json:"sha256"`
	// TODO: we should figure out how to compute integrity out of sha256
	Urls []string    `json:"urls"`
}

type BzlModLockFile struct {
Name string          `json:"name"`
BaseSystem string    `json:"base-system"`
BuildFile string     `json:"build-file"`
RepoFiles []string   `json:"repo-files"`
Arch string          `json:"arch"`
Required []string    `json:"required"`
Version int          `json:"version"`
ForceIgnoreWithDependencies []string `json:"force-ignore-with-dependencies"`

Rpms []BzlModLockFileRPM `json:"rpms"`
}

const CURRENT_LOCK_FILE_VERSION = 2

func LoadBzlModLockFile(path string) (*BzlModLockFile, error) {
	_, err := os.Stat(path)

	if os.IsNotExist(err) {
		return nil, nil
	}

	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	lock := &BzlModLockFile{}
	err = json.Unmarshal(file, &lock)
	if err != nil {
		return nil, err
	}

	if lock.Version != CURRENT_LOCK_FILE_VERSION {
		// TODO: maybe we should have a migration path
		return nil, fmt.Errorf("lock file version %d is not supported, expected %d", lock.Version, CURRENT_LOCK_FILE_VERSION)
	}

	sort.Slice(lock.Required, func(i, j int) bool {
		return lock.Required[i] < lock.Required[j]
	})

	return lock, nil
}

func UpdateBzlModLockFile(lockContent *BzlModLockFile, lockFile string, pkgs []*api.Package, arch string) error {
	var rpms []BzlModLockFileRPM

	sort.Slice(pkgs, func(i, j int) bool {
		return pkgs[i].String() < pkgs[j].String()
	})

	for _, pkg := range pkgs {
		pkgName := sanitize(pkg.String() + "." + arch)
		urls, err := ComputeMirrorURLs(pkg.Repository.Mirrors, pkg.Location.Href)
		if err != nil {
			return err
		}
		line := BzlModLockFileRPM{ Name: pkgName, Sha256: pkg.Checksum.Text, Urls: urls }
		rpms = append(rpms, line)
	}

	lockContent.Rpms = rpms

	lockContent.Version = CURRENT_LOCK_FILE_VERSION

	data, err := json.MarshalIndent(lockContent, "", "    ")
	if err != nil {
		return err
	}

	return os.WriteFile(lockFile, data, 0644)

}

func addRepoUsage(module *build.File, proxies []string, repo string) *build.File {

	useRepos := bzlmod.UseRepos(module, proxies)
	if len(useRepos) == 0 {
		var newUseRepo *build.CallExpr
		module, newUseRepo = bzlmod.NewUseRepo(module, proxies)
		useRepos = []*build.CallExpr{newUseRepo}
	}

	bzlmod.AddRepoUsages(useRepos, repo)

	return module
}
