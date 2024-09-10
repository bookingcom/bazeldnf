"""Extensions for bzlmod.

Installs the bazeldnf toolchain.

based on: https://github.com/bazel-contrib/rules-template/blob/0dadcb716f06f672881681155fe6d9ff6fc4a4f4/mylang/extensions.bzl
"""

load("@bazel_features//:features.bzl", "bazel_features")
load("//internal:rpm.bzl", rpm_repository = "rpm")
load(":repositories.bzl", "bazeldnf_register_toolchains")

_ALIAS_TEMPLATE = """\
alias(
    name = "{name}",
    actual = "@{actual_name}//rpm",
    visibility = ["//visibility:public"],
)
"""

_ALIAS_REPO_TOP_LEVEL_TEMPLATE = """\
load("@bazeldnf//bazeldnf/private:lock-file-helpers.bzl", "update_lock_file")

update_lock_file(
    name = "update-lock-file",
    lock_file = "{path}",
    rpms = [{rpms}],
    excludes = [{excludes}],
    repofile = "{repofile}",
)
"""

_UPDATE_LOCK_FILE_TEMPLATE = """\
fail("Lock file hasn't been generated for this repository, please run `bazel run @{repo}//:update-lock-file` first")
"""

def _alias_repository_impl(repository_ctx):
    """Creates a repository that aliases other repositories."""
    repository_ctx.file("WORKSPACE", "")
    lock_file_path = repository_ctx.attr.lock_file.name

    repofile = repository_ctx.attr.repofile.name if repository_ctx.attr.repofile else "invalid-repo.yaml"

    if repository_ctx.attr.lock_file.package:
        lock_file_path = repository_ctx.attr.lock_file.package + "/" + lock_file_path
        repofile = repository_ctx.attr.repofile.package + "/" + repofile

    repository_ctx.file(
        "BUILD.bazel",
        _ALIAS_REPO_TOP_LEVEL_TEMPLATE.format(
            path = lock_file_path,
            rpms = ", ".join(["'{}'".format(x) for x in repository_ctx.attr.rpms_to_install]),
            excludes = ", ".join(["'{}'".format(x) for x in repository_ctx.attr.excludes]),
            repofile = repofile,
        ),
    )
    for rpm in repository_ctx.attr.rpms:
        actual_name = rpm.repo_name
        name = actual_name.split(repository_ctx.attr.repository_prefix, 1)[1]
        repository_ctx.file(
            "%s/BUILD.bazel" % name,
            _ALIAS_TEMPLATE.format(
                name = name,
                actual_name = actual_name,
            ),
        )

    if not repository_ctx.attr.rpms:
        for rpm in repository_ctx.attr.rpms_to_install:
            repository_ctx.file(
                "%s/BUILD.bazel" % rpm,
                _UPDATE_LOCK_FILE_TEMPLATE.format(
                    repo = repository_ctx.name.rsplit("~", 1)[-1],
                ),
            )

_alias_repository = repository_rule(
    implementation = _alias_repository_impl,
    attrs = {
        "rpms": attr.label_list(default = []),
        "lock_file": attr.label(),
        "rpms_to_install": attr.string_list(),
        "excludes": attr.string_list(),
        "repofile": attr.label(),
        "repository_prefix": attr.string(),
    },
)

_DEFAULT_NAME = "bazeldnf"

def _handle_lock_file(config, module_ctx, registered_rpms = {}):
    if not config.lock_file:
        fail("No lock file provided for %s" % config.name)

    repository_args = {
        "name": config.name,
        "lock_file": config.lock_file,
        "rpms_to_install": config.rpms,
        "excludes": config.excludes,
        "repofile": config.repofile,
        "repository_prefix": config.rpm_repository_prefix,
    }

    if not module_ctx.path(config.lock_file).exists:
        _alias_repository(
            **repository_args
        )
        return config.name

    content = module_ctx.read(config.lock_file)
    lock_file_json = json.decode(content)

    for rpm in lock_file_json.get("packages", []):
        dependencies = rpm.pop("dependencies", [])
        dependencies = [x.replace("+", "plus") for x in dependencies]
        dependencies = ["@{}{}//rpm".format(config.rpm_repository_prefix, x) for x in dependencies]
        name = rpm.pop("name").replace("+", "plus")
        name = "{}{}".format(config.rpm_repository_prefix, name)
        if name in registered_rpms:
            continue
        registered_rpms[name] = 1
        repository = rpm.pop("repository")
        mirrors = lock_file_json.get("repositories", {}).get(repository, None)
        if mirrors == None:
            fail("couldn't resolve %s in %s" % (repository, lock_file_json["repositories"]))
        href = rpm.pop("href")
        urls = ["%s/%s" % (x, href) for x in mirrors]
        rpm_repository(
            name = name,
            dependencies = dependencies,
            urls = urls,
            **rpm
        )

    repository_args["rpms"] = ["@@%s//rpm" % x for x in registered_rpms.keys()]

    _alias_repository(
        **repository_args
    )

    return config.name

def _toolchain_extension(module_ctx):
    repos = []

    for mod in module_ctx.modules:
        for toolchain in mod.tags.toolchain:
            if toolchain.name != _DEFAULT_NAME and not mod.is_root:
                fail("""\
                Only the root module may override the default name for the bazeldnf toolchain.
                This prevents conflicting registrations in the global namespace of external repos.
                """)
            if mod.is_root and toolchain.disable:
                break
            bazeldnf_register_toolchains(
                name = toolchain.name,
                register = False,
            )
            if mod.is_root:
                repos.append(toolchain.name + "_toolchains")

        legacy = True
        name = "bazeldnf_rpms"
        registered_rpms = dict()
        for config in mod.tags.config:
            repos.append(
                _handle_lock_file(
                    config,
                    module_ctx,
                    registered_rpms,
                ),
            )

        rpms = []

        for rpm in mod.tags.rpm:
            rpm_repository(
                name = rpm.name,
                urls = rpm.urls,
                sha256 = rpm.sha256,
                integrity = rpm.integrity,
            )

            if mod.is_root and legacy:
                repos.append(rpm.name)
            else:
                rpms.append(rpm.name)

        if not legacy and rpms:
            _alias_repository(
                name = name,
                rpms = ["@@%s//rpm" % x for x in rpms],
            )
            repos.append(name)

    kwargs = {}
    if bazel_features.external_deps.extension_metadata_has_reproducible:
        kwargs["reproducible"] = True

    if module_ctx.root_module_has_non_dev_dependency:
        kwargs["root_module_direct_deps"] = repos
        kwargs["root_module_direct_dev_deps"] = []
    else:
        kwargs["root_module_direct_deps"] = []
        kwargs["root_module_direct_dev_deps"] = repos

    return module_ctx.extension_metadata(**kwargs)

_toolchain_tag = tag_class(
    attrs = {
        "name": attr.string(
            doc = """\
Base name for generated repositories, allowing more than one bazeldnf toolchain to be registered.
Overriding the default is only permitted in the root module.
""",
            default = _DEFAULT_NAME,
        ),
        "disable": attr.bool(default = False),
    },
    doc = "Allows registering a prebuilt bazeldnf toolchain",
)

_rpm_tag = tag_class(
    attrs = {
        "name": attr.string(doc = "Name of the generated repository"),
        "urls": attr.string_list(doc = "URLs from which to download the RPM file"),
        "sha256": attr.string(doc = """\
The expected SHA-256 of the file downloaded.
This must match the SHA-256 of the file downloaded.
_It is a security risk to omit the SHA-256 as remote files can change._
At best omitting this field will make your build non-hermetic.
It is optional to make development easier but either this attribute or
`integrity` should be set before shipping.
"""),
        "integrity": attr.string(doc = """\
Expected checksum in Subresource Integrity format of the file downloaded.
This must match the checksum of the file downloaded.
_It is a security risk to omit the checksum as remote files can change._
At best omitting this field will make your build non-hermetic.
It is optional to make development easier but either this attribute or
`sha256` should be set before shipping.
"""),
    },
    doc = "Allows registering a Bazel repository wrapping an RPM file",
)

_config_tag = tag_class(
    attrs = {
        "name": attr.string(
            doc = "Name of the generated proxy repository",
            default = "bazeldnf_rpms",
        ),
        "lock_file": attr.label(
            doc = """\
Label of the JSON file that contains the RPMs to expose, there's no legacy mode \
for RPMs defined by a lock file.

The lock file content is as: TBD
""",
            allow_single_file = [".json"],
        ),
        "rpm_repository_prefix": attr.string(
            doc = "A prefix to add to all generated rpm repositories",
            default = "",
        ),
        "repofile": attr.label(
            doc = "YAML file that defines the repositories used for this lock file",
            allow_single_file = [".yaml"],
        ),
        "rpms": attr.string_list(
            doc = "name of the RPMs to install",
        ),
        "excludes": attr.string_list(
            doc = "Regex to pass to bazeldnf to exclude from the dependency tree",
        ),
    },
)

bazeldnf = module_extension(
    implementation = _toolchain_extension,
    tag_classes = {
        "toolchain": _toolchain_tag,
        "rpm": _rpm_tag,
        "config": _config_tag,
    },
)
