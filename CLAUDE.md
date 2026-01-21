# bazeldnf

## Project Overview

**bazeldnf** is a Bazel library for managing the complete RPM dependency lifecycle using pure Go and Starlark rules. It enables building minimal container images with RPM dependencies without requiring external tools like `dnf` or `yum`.

- **License**: Apache 2.0 (Copyright 2022 The KubeVirt Authors)
- **Primary Use Case**: Building minimal scratch-based containers with only required RPM packages
- **Key Technologies**: Bazel, Go 1.24.1, Starlark rules, SAT solver
- **Repository**: https://github.com/rmohr/bazeldnf

## Repository Structure

```
/bazeldnf/          # Public Starlark API and bzlmod extensions
  ├── defs.bzl      # Main exports: rpm, rpmtree, tar2files, xattrs, bazeldnf
  ├── extensions.bzl # Bzlmod extensions (bazeldnf_toolchain, bazeldnf)
  ├── toolchain.bzl # Toolchain wrapper
  ├── platforms.bzl # Platform definitions
  └── repositories.bzl # Dependency setup

/internal/          # Internal Starlark rule implementations
  ├── rpm.bzl       # _rpm repository rule and RpmInfo provider
  ├── rpmtree.bzl   # _rpm2tar and _tar2files rules
  ├── xattrs.bzl    # _xattrs rule
  └── bazeldnf.bzl  # bazeldnf executable rule

/pkg/               # Go packages
  ├── api/          # RPM metadata XML parsing, data models
  ├── repo/         # Repository handling (YAML config)
  ├── sat/          # SAT solver integration for dependency resolution
  ├── reducer/      # Repository reduction for relevant packages
  ├── bazel/        # WORKSPACE/BUILD file manipulation (AST)
  ├── rpm/          # RPM utilities (version comparison, keys)
  ├── ldd/          # Shared library dependency analysis
  └── xattr/        # Extended attributes handling

/cmd/               # CLI implementation (Cobra commands)
  ├── rpmtree/      # Dependency resolution
  ├── init/         # Repository initialization
  ├── fetch/        # RPM downloading
  ├── prune/        # Clean unreferenced RPMs
  └── ldd/          # Library dependency analysis

/e2e/               # End-to-end test projects (7 scenarios)
  ├── bazel-workspace/
  ├── bazel-bzlmod/
  ├── bazel-bzlmod-lock-file/
  ├── bazel-bzlmod-lock-file-from-args/
  ├── bazel-bzlmod-toolchain-from-source/
  ├── bazel-bzlmod-toolchain-from-source-lock-file/
  └── bzlmod-toolchain-circular-dependencies/

/tools/             # Build system tools
/docs/              # Documentation
```

## Build System

- **Primary Mode**: Bazel with bzlmod (MODULE.bazel) for Bazel 7.x+
- **Legacy Support**: WORKSPACE mode for Bazel 6.x
- **Go Version**: 1.24.1 (managed by rules_go)
- **Bazel Compatibility**: >= 7.0.0
- **Module Name**: `bazeldnf` (version: v0.0.0)

**Key Dependencies**:
- `bazel_skylib` - Utility functions
- `rules_go` - Go compilation
- `gazelle` - Go dependency management
- `platforms` - Platform constraints
- `rules_pkg` - Package building

## Public API

All public rules are exported from [bazeldnf/defs.bzl](bazeldnf/defs.bzl):

### 1. `rpm` - Repository Rule

Downloads and manages individual RPM files.

**Parameters**:
- `urls` (string_list, mandatory) - Download URLs for the RPM
- `sha256` (string, optional) - Expected SHA-256 hash
- `integrity` (string, optional) - Subresource Integrity checksum (alternative to sha256)
- `dependencies` (label_list, optional) - Other rpm targets this depends on
- `auth_patterns` (string_dict, optional) - Authentication patterns for URLs

**Example**:
```python
load("@bazeldnf//bazeldnf:defs.bzl", "rpm")

rpm(
    name = "libvirt-libs-11.0.0-1.fc42.x86_64.rpm",
    sha256 = "aac272a2ace134b5ef60a41e6624deb24331e79c76699ef6cef0dca22d94ac7e",
    urls = [
        "https://kojipkgs.fedoraproject.org/packages/libvirt/11.0.0/1.fc42/x86_64/libvirt-libs-11.0.0-1.fc42.x86_64.rpm",
    ],
)
```

**Location**: [internal/rpm.bzl](internal/rpm.bzl)

### 2. `rpmtree` - Macro

Merges multiple RPM files into a single tar archive with optional symlinks and extended attributes.

**Parameters**:
- `name` (string, required) - Target name
- `rpms` (label_list, required) - List of RPM targets with RpmInfo provider
- `symlinks` (string_dict, optional) - Relative symlinks `{"/path/link": "../target"}`
- `capabilities` (string_list_dict, optional) - Linux capabilities `{"/usr/bin/foo": ["cap_net_bind_service"]}`
- `selinux_labels` (string_dict, optional) - SELinux labels for files

**Example**:
```python
load("@bazeldnf//bazeldnf:defs.bzl", "rpmtree")

rpmtree(
    name = "rpmarchive",
    rpms = [
        "@libvirt-libs-11.0.0-1.fc42.x86_64.rpm//rpm",
        "@libvirt-devel-11.0.0-1.fc42.x86_64.rpm//rpm",
    ],
    symlinks = {
        "/var/run": "../run",
    },
    capabilities = {
        "/usr/libexec/qemu-kvm": ["cap_net_bind_service"],
    },
)
```

**Output**: Produces a `.tar` file that can be used directly in container_layer or pkg_tar.

**Location**: [internal/rpmtree.bzl](internal/rpmtree.bzl)

### 3. `tar2files` - Macro

Extracts specific files from a tar archive for selective exposure to build targets.

**Parameters**:
- `name` (string, required) - Base target name
- `tar` (label, required) - Input tar file (typically from rpmtree)
- `files` (dict, required) - Directory paths mapped to file lists
  - Keys: Paths within tar (e.g., "/usr/lib64", "/usr/include/libvirt")
  - Values: List of filenames to extract

**Example**:
```python
load("@bazeldnf//bazeldnf:defs.bzl", "tar2files")

tar2files(
    name = "libvirt-libs",
    files = {
        "/usr/include/libvirt": [
            "libvirt-admin.h",
            "libvirt-common.h",
            "libvirt-domain.h",
        ],
        "/usr/lib64": [
            "libvirt.so.0",
            "libvirt.so.0.11000.0",
        ],
    },
    tar = ":rpmarchive",
    visibility = ["//visibility:public"],
)

# Use with cc_library
cc_library(
    name = "libvirt",
    srcs = [":libvirt-libs/usr/lib64"],
    hdrs = [":libvirt-libs/usr/include/libvirt"],
    strip_include_prefix = "/libvirt-libs/",
)
```

**Why needed**: Prevents Bazel from trying to link all libraries in an RPM tree, allowing selective exposure.

**Location**: [internal/rpmtree.bzl](internal/rpmtree.bzl)

### 4. `xattrs` - Macro

Modifies extended attributes (capabilities, SELinux labels) on tar file members.

**Parameters**:
- `name` (string, required) - Target name
- `tar` (label, required) - Input tar file
- `capabilities` (string_list_dict, optional) - Linux capabilities configuration
- `selinux_labels` (string_dict, optional) - SELinux labels configuration

**Location**: [internal/xattrs.bzl](internal/xattrs.bzl)

### 5. `bazeldnf` - Executable Rule

Runs the bazeldnf CLI tool as a Bazel target.

**Parameters**:
- `name` (string, required) - Target name
- `command` (string, optional) - One of `""`, `"ldd"`, `"sandbox"`
- `rpmtree` (label, optional) - Reference to rpmtree target (for ldd)
- `libs` (string_list, optional) - Library paths for ldd analysis
- `rulename` (string, optional) - Name for generated tar2files target (for ldd)

**Example - LDD mode** (auto-generates transitive library dependencies):
```python
load("@bazeldnf//bazeldnf:defs.bzl", "bazeldnf")

bazeldnf(
    name = "ldd",
    command = "ldd",
    libs = [
        "/usr/lib64/libvirt-lxc.so.0",
        "/usr/lib64/libvirt-qemu.so.0",
        "/usr/lib64/libvirt.so.0",
    ],
    rpmtree = ":libvirt-devel",
    rulename = "libvirt-libs",
)

# Run: bazel run //:ldd
# This updates the tar2files target with all transitive dependencies
```

**Location**: [internal/bazeldnf.bzl](internal/bazeldnf.bzl)

## CLI Commands

The bazeldnf CLI is built in Go using Cobra. Run commands via:
```bash
bazel run //:bazeldnf -- <command> [flags]
```

### Key Commands

| Command | Purpose |
|---------|---------|
| `init` | Initialize repo.yaml configuration for Fedora repos |
| `rpmtree` | Resolve RPM dependencies and generate WORKSPACE/lock files |
| `fetch` | Download RPM files from repositories |
| `prune` | Remove unreferenced RPM declarations from WORKSPACE/BUILD files |
| `ldd` | Analyze shared library dependencies |
| `resolve` | Low-level dependency resolution |
| `verify` | Verify dependencies are satisfied |
| `lockfile` | Manage lock files |
| `reduce` | Trim repositories for testing (debug command) |
| `rpm2tar` | Convert RPM to tar (internal, used by rpmtree rule) |
| `tar2files` | Extract files from tar (internal, used by tar2files rule) |
| `xattr` | Manage extended attributes (internal) |

### Common CLI Workflows

**Initialize repository configuration**:
```bash
bazel run //:bazeldnf -- init --fc 42
# Creates repo.yaml with Fedora 42 repositories
```

**Resolve dependencies and update WORKSPACE**:
```bash
bazel run //:bazeldnf -- rpmtree --workspace /my/WORKSPACE --buildfile /my/BUILD.bazel --name libvirttree libvirt
```

**Resolve dependencies and create lock file**:
```bash
bazel run //:bazeldnf -- rpmtree --lockfile rpms.json --configname myrpms --name libvirttree libvirt
```

**Prune unreferenced RPMs**:
```bash
bazel run //:bazeldnf -- prune --workspace /my/WORKSPACE --buildfile /my/BUILD.bazel
```

**Flags**:
- `--nobest` - Allow older package versions when newest causes conflicts
- `--repofile` - Specify repo.yaml location (default: repo.yaml)

## Core Architecture

### RpmInfo Provider

Custom Starlark provider for tracking RPM dependencies:
```python
RpmInfo = provider(
    fields = {
        "deps": "depset of other dependencies",
        "file": "label of the RPM file",
    },
)
```

Used throughout the rule chain to ensure proper transitive dependency resolution.

### SAT Solver

bazeldnf uses a Boolean satisfiability (SAT) solver for optimal package resolution:

- **Library**: [gophersat](https://github.com/crillab/gophersat) (MaxSAT solver)
- **Purpose**: Convert RPM dependency constraints to Boolean formulas
- **Location**: [pkg/sat/](pkg/sat/)
- **Strategy**:
  - Prefers newest packages (highest weight in MaxSAT)
  - Finds minimal set satisfying all dependencies
  - Handles complex multi-repository scenarios

### Toolchain System

Platform-specific bazeldnf binaries managed via Bazel toolchains:

**Supported Platforms**:
- darwin-amd64
- linux-amd64
- linux-arm64
- linux-ppc64le
- linux-s390x
- linux-riscv64

**Modes**:
1. **Prebuilt toolchain** (default) - Downloads platform-specific binary
2. **Build from source** - Compiles Go binary during MODULE.bazel evaluation

**Toolchain registration**:
```python
# In MODULE.bazel
bazeldnf_toolchain = use_extension("//bazeldnf:extensions.bzl", "bazeldnf_toolchain")
bazeldnf_toolchain.register()
use_repo(bazeldnf_toolchain, "bazeldnf_toolchains")
register_toolchains("@bazeldnf_toolchains//:all")
```

### Lock File Format

JSON-based format for bzlmod mode (alternative to WORKSPACE):

```json
{
    "name": "bazeldnf-rpms",
    "cli-arguments": ["libvirt", "bash"],
    "repositories": {
        "fedora-42-updates": [
            "https://mirrors.fedoraproject.org/metalink?repo=updates&arch=x86_64"
        ]
    },
    "rpms": [
        {
            "name": "libvirt-libs",
            "integrity": "sha256-qsJypK7hNLXvYGRm8kJORxvG5e9+788pou1UKt0p2vo=",
            "urls": ["libvirt/11.0.0/1.fc42/x86_64/libvirt-libs-11.0.0-1.fc42.x86_64.rpm"],
            "repository": "fedora-42-updates"
        }
    ],
    "targets": ["libvirt", "bash"]
}
```

**Benefits**:
- Reproducible builds across environments
- Easier to debug than generated WORKSPACE files
- Version-controlled dependency snapshots
- Only works in bzlmod mode (Bazel 7.x+)

### Bzlmod Extensions

Two extensions in [bazeldnf/extensions.bzl](bazeldnf/extensions.bzl):

#### 1. `bazeldnf_toolchain` Extension
Registers prebuilt or source-built toolchain.

**Tags**:
- `register()` - Register prebuilt toolchain
- `register_from_source()` - Build toolchain from source

#### 2. `bazeldnf` Extension
Configures RPM repositories.

**Tags**:
- `rpm(name, sha256, urls)` - Individual RPM definition
- `config(name, lock_file, repofile, rpms)` - Lock file-based configuration

**Example**:
```python
# MODULE.bazel
bazeldnf = use_extension("@bazeldnf//bazeldnf:extensions.bzl", "bazeldnf")
bazeldnf.config(
    name = "bazeldnf_rpms",
    lock_file = "//:rpms.json",
    repofile = "//:repo.yaml",
    rpms = ["libvirt", "bash"],
)
use_repo(bazeldnf, "bazeldnf_rpms")
```

## Go Packages

### [pkg/api/](pkg/api/)
RPM metadata parsing and data models.

- **api.go** - XML parsing for RPM repository metadata (repomd.xml, primary.xml)
- **bazeldnf/** - Protocol buffer and config definitions
- Handles package, repository, and version data structures

### [pkg/repo/](pkg/repo/)
RPM repository handling.

- YAML format repository configuration (repo.yaml)
- Metalink support for mirror discovery
- Repository file loading and querying

### [pkg/sat/](pkg/sat/)
SAT solver integration for dependency resolution.

- Converts RPM dependencies (Provides/Requires) to Boolean formulas
- Uses MaxSAT to find optimal solutions
- Handles version constraints and conflicts
- Prefers newest packages via weighted clauses

### [pkg/reducer/](pkg/reducer/)
Repository reduction for efficient dependency resolution.

- Filters repositories to relevant packages only
- Performs transitive dependency analysis
- Reduces search space for SAT solver

### [pkg/bazel/](pkg/bazel/)
Bazel file manipulation.

- Parses and modifies WORKSPACE, BUILD, and .bzl files
- Uses buildtools library for AST manipulation
- Handles RPM declaration insertion and pruning
- Generates formatted Starlark code

### [pkg/rpm/](pkg/rpm/)
RPM utilities.

- Version comparison (epoch-version-release)
- Package key management
- RPM naming conventions

### [pkg/ldd/](pkg/ldd/)
Shared library dependency analysis.

- Introspects ELF binaries for library requirements
- Generates tar2files rules with transitive dependencies
- Updates BUILD files with discovered libraries

### [pkg/xattr/](pkg/xattr/)
Extended attributes handling.

- Linux capabilities (cap_net_bind_service, etc.)
- SELinux labels
- Tar archive attribute modification

## E2E Testing

Seven test projects in [e2e/](e2e/) covering different scenarios:

| Test Project | Bazel Versions | Purpose |
|--------------|----------------|---------|
| `bazel-workspace` | 6.x, 7.x | WORKSPACE mode compatibility |
| `bazel-bzlmod` | 7.x, 8.x | Basic bzlmod with prebuilt toolchain |
| `bazel-bzlmod-lock-file` | 7.x, 8.x | Lock file-based dependency resolution |
| `bazel-bzlmod-lock-file-from-args` | 7.x, 8.x | CLI-generated lock files |
| `bazel-bzlmod-toolchain-from-source` | 7.x, 8.x | Building toolchain from Go source |
| `bazel-bzlmod-toolchain-from-source-lock-file` | 7.x, 8.x | Source-built toolchain + lock file |
| `bzlmod-toolchain-circular-dependencies` | 7.x, 8.x | Circular dependency handling (experimental) |

**Test Execution**:
- **GitHub Actions**: Matrix testing across Bazel versions
- **Caching**: Aggressive caching (bazelisk, disk, repository, external)
- **CI Config**: `.aspect/bazelrc/ci.bazelrc`, `.github/workflows/ci.bazelrc`

**Test Structure** (each project):
```
e2e/<test-name>/
├── MODULE.bazel          # Module with local_path_override to ../..
├── BUILD.bazel           # rpmtree, tar2files, cc_library examples
├── WORKSPACE.bzlmod      # Empty marker for bzlmod mode
├── repo.yaml             # Repository configuration (optional)
└── *.json                # Lock files (optional)
```

**Run E2E tests**:
```bash
cd e2e/bazel-bzlmod
bazelisk build //...
```

## Common Workflows

### Workflow 1: Adding RPM Dependencies (Bzlmod + Lock File)

```bash
# 1. Initialize repository configuration
bazel run //:bazeldnf -- init --fc 42

# 2. Resolve dependencies and create lock file
bazel run //:bazeldnf -- rpmtree --lockfile rpms.json --configname myrpms --name libvirttree libvirt

# 3. Configure in MODULE.bazel
```

```python
# MODULE.bazel
bazeldnf = use_extension("@bazeldnf//bazeldnf:extensions.bzl", "bazeldnf")
bazeldnf.config(
    name = "bazeldnf_rpms",
    lock_file = "//:rpms.json",
    repofile = "//:repo.yaml",
)
use_repo(bazeldnf, "bazeldnf_rpms")
```

```python
# BUILD.bazel
load("@bazeldnf//bazeldnf:defs.bzl", "rpmtree")

rpmtree(
    name = "rpmarchive",
    rpms = ["@bazeldnf_rpms//libvirt-libs"],
)
```

### Workflow 2: Extracting Libraries for cc_library

```python
load("@bazeldnf//bazeldnf:defs.bzl", "tar2files")

tar2files(
    name = "libvirt-libs",
    files = {
        "/usr/include/libvirt": [
            "libvirt.h",
            "libvirt-domain.h",
            "libvirt-event.h",
        ],
        "/usr/lib64": [
            "libvirt.so.0",
            "libvirt.so.0.11000.0",
        ],
    },
    tar = ":rpmarchive",
    visibility = ["//visibility:public"],
)

cc_library(
    name = "mylib",
    srcs = [":libvirt-libs/usr/lib64"],
    hdrs = [":libvirt-libs/usr/include/libvirt"],
    strip_include_prefix = "/libvirt-libs/",
)
```

### Workflow 3: Auto-Generating Transitive Library Dependencies

```python
load("@bazeldnf//bazeldnf:defs.bzl", "bazeldnf", "tar2files")

# Define target to introspect
bazeldnf(
    name = "ldd",
    command = "ldd",
    libs = [
        "/usr/lib64/libvirt-lxc.so.0",
        "/usr/lib64/libvirt-qemu.so.0",
        "/usr/lib64/libvirt.so.0",
    ],
    rpmtree = ":libvirt-devel",
    rulename = "libvirt-libs",
)

# Run introspection
# bazel run //:ldd
# This updates the tar2files target with all transitive dependencies

tar2files(
    name = "libvirt-libs",
    files = {
        # Will be populated by bazel run //:ldd
    },
    tar = ":libvirt-devel",
)
```

### Workflow 4: Building Container Images

```python
load("@bazeldnf//bazeldnf:defs.bzl", "rpmtree")
load("@rules_pkg//pkg:tar.bzl", "pkg_tar")

rpmtree(
    name = "base-rpms",
    rpms = [
        "@bazeldnf_rpms//bash",
        "@bazeldnf_rpms//coreutils",
    ],
)

container_layer(
    name = "base-layer",
    tars = [":base-rpms"],
)
```

## Key Patterns

### File Organization

- **Public API**: [bazeldnf/defs.bzl](bazeldnf/defs.bzl) exports all user-facing rules
- **Internal Implementations**: [internal/*.bzl](internal/) (prefixed with `_`)
- **Extensions**: [bazeldnf/extensions.bzl](bazeldnf/extensions.bzl) for bzlmod configuration
- **Private Helpers**: [bazeldnf/private/](bazeldnf/private/) (lock file helpers, toolchain repo)

### Macro vs Rule Pattern

- **Macros** (user-facing): `rpmtree`, `tar2files`, `xattrs`
  - Generate rules with predictable naming
  - Example: `rpmtree(name="foo")` → creates rule "foo" with output "foo.tar"

- **Rules** (internal): `_rpm2tar`, `_tar2files`, `_xattrs`
  - Underscore prefix indicates internal use
  - Called by corresponding macros

- **Repository Rules**: `rpm`, `null_rpm`
  - Download/manage external artifacts

### Toolchain Integration

All tool invocations use the toolchain pattern:

```python
ctx.toolchains[BAZELDNF_TOOLCHAIN]._tool
```

This ensures platform-specific binary selection and supports both prebuilt and source-built modes.

### Authentication

**Build-time** (Bazel downloading RPMs):
- Bazel's `.netrc` file
- Credential helper mechanism

**Resolution-time** (bazeldnf CLI resolving dependencies):
- Reads `$NETRC` environment variable
- Falls back to `~/.netrc`
- Supports basic auth

## Testing Guidelines

### Adding E2E Tests

1. **Create test directory**: `e2e/<descriptive-name>/`
2. **Add MODULE.bazel**:
   ```python
   module(name = "example-bazeldnf-<feature>")

   bazel_dep(name = "bazeldnf", dev_dependency = True)
   local_path_override(
       module_name = "bazeldnf",
       path = "../..",
   )
   ```
3. **Add BUILD.bazel**: Include rpmtree, tar2files examples
4. **Add to CI**: Update `.github/workflows/action.yml` with matrix job
5. **Use minimal RPMs**: Test with standard packages (e.g., libvirt, bash)

### Go Unit Tests

**Framework**: [Gomega](https://github.com/onsi/gomega) (BDD-style assertions)

**Pattern**:
```go
package mypackage

import (
    "testing"
    . "github.com/onsi/gomega"
)

func TestFeature(t *testing.T) {
    g := NewGomegaWithT(t)

    result := functionUnderTest(input)

    g.Expect(result).Should(Equal(expected))
    g.Expect(err).Should(BeNil())
}

// Table-driven tests
func TestMultipleScenarios(t *testing.T) {
    tests := []struct {
        name     string
        input    interface{}
        expected interface{}
    }{
        {"scenario1", input1, expected1},
        {"scenario2", input2, expected2},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            g := NewGomegaWithT(t)
            result := functionUnderTest(tt.input)
            g.Expect(result).Should(Equal(tt.expected))
        })
    }
}
```

**Location**: `*_test.go` files alongside implementation

## Development Setup

### Prerequisites

- **Bazel**: 7.0.0 or higher (recommend [bazelisk](https://github.com/bazelbuild/bazelisk))
- **Go**: 1.24.1 (automatically managed by rules_go)

### Building

```bash
# Build everything
bazel build //...

# Build specific target
bazel build //cmd:cmd
```

### Testing

```bash
# Run all tests
bazel test //...

# Run specific test
bazel test //pkg/sat:sat_test
```

### Running E2E Tests

```bash
cd e2e/bazel-bzlmod
bazelisk build //...
```

### Running the CLI

```bash
# Via Bazel
bazel run //:bazeldnf -- --help

# Specific command
bazel run //:bazeldnf -- rpmtree --help
```

## Important Notes for Claude Code

### When Reading Code

- **Public API**: Check [bazeldnf/defs.bzl](bazeldnf/defs.bzl) for user-facing surface
- **Implementations**: Look in [internal/](internal/) for rule logic (underscore prefixes)
- **Go CLI**: Commands are in [cmd/](cmd/) subdirectories using Cobra
- **SAT Solver**: Complex logic in [pkg/sat/](pkg/sat/) - uses Boolean satisfiability
- **AST Manipulation**: [pkg/bazel/](pkg/bazel/) uses buildtools for Starlark parsing

### When Making Changes

**Starlark rules**:
- Public API changes → update [bazeldnf/defs.bzl](bazeldnf/defs.bzl)
- Rule implementation → modify [internal/*.bzl](internal/)
- Always update corresponding tests

**Go code**:
- CLI changes → [cmd/](cmd/) with Cobra command structure
- Library changes → [pkg/](pkg/) with unit tests
- Use Gomega for test assertions

**Lock file format**:
- Changes impact [bazeldnf/extensions.bzl](bazeldnf/extensions.bzl)
- Update lock file parsing in [pkg/api/](pkg/api/)
- Test with e2e scenarios

**Testing**:
- Both Go unit tests (`*_test.go`) and e2e scenarios required
- Test across Bazel versions (CI matrix)
- Update [README.md](README.md) examples if API changes

### When Adding Features

**Considerations**:
- Support both WORKSPACE and bzlmod modes
- Test across multiple Bazel versions (6.x, 7.x, 8.x)
- Update [README.md](README.md) with examples
- Add e2e test scenario for significant features
- Use RpmInfo provider for dependency tracking
- Maintain toolchain compatibility (prebuilt + source builds)

**Example - Adding a new rule**:
1. Implement in [internal/newrule.bzl](internal/)
2. Export in [bazeldnf/defs.bzl](bazeldnf/defs.bzl)
3. Add unit tests
4. Create e2e test scenario in [e2e/](e2e/)
5. Update [README.md](README.md)

### Key Constraints

**Design Principles**:
- **No external dependencies**: Must work without dnf/yum (pure Go)
- **Minimal containers**: Designed for scratch base images
- **Hermetic builds**: All dependencies resolved via SAT solver

**RPM Limitations**:
- Ignores: `recommends`, `supplements`, `suggests`, `enhances`
- Does not resolve boolean logic in `requires` (e.g., `gcc if something`)
- Only handles straightforward dependency chains

**Deliberately Not Supported**:
- RPM triggers
- Pre/post install scripts requiring system interaction
- Package recommendations (only hard requirements)

### Debugging Tips

**CLI Testing**:
```bash
# Run with verbose output
bazel run //:bazeldnf -- rpmtree --help

# Test dependency resolution
bazel run //:bazeldnf -- resolve --repofile repo.yaml libvirt
```

**Common Issues**:
- **Dependency conflicts**: Use `--nobest` flag to allow older packages
- **Lock file errors**: Easier to debug than WORKSPACE mode
- **SAT solver verbose**: Check [pkg/sat/](pkg/sat/) for logging
- **Library linking**: Use `bazeldnf ldd` to auto-generate dependencies

**Useful Commands**:
```bash
# Check what would be resolved
bazel run //:bazeldnf -- resolve --repofile repo.yaml <package>

# Verify lock file
bazel run //:bazeldnf -- verify --lockfile rpms.json

# Update lock file
bazel run @bazeldnf_rpms//:update-lock-file
```

**File Locations for Common Tasks**:
- Rule behavior: [internal/*.bzl](internal/)
- Dependency resolution: [pkg/sat/](pkg/sat/), [pkg/reducer/](pkg/reducer/)
- File generation: [pkg/bazel/](pkg/bazel/)
- Extension logic: [bazeldnf/extensions.bzl](bazeldnf/extensions.bzl)

## Additional Resources

- **README**: [README.md](README.md) - User documentation
- **GitHub**: https://github.com/rmohr/bazeldnf
- **Issues**: https://github.com/rmohr/bazeldnf/issues
- **Releases**: https://github.com/rmohr/bazeldnf/releases

---

**Last Updated**: 2026-01-21
**Version**: Based on bazeldnf v0.0.0 (main branch)
