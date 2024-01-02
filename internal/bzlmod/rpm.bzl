load("//:deps.bzl", _rpm_repository = "rpm")

__BUILD_FILE_CONTENT__ = """
load("@{repository_name}//:deps.bzl", "rpmtree")

rpmtree(
    name = "rpms",
    rpms = [
        {rpms}
    ],
    visibility = [
        {visibility}
    ]
)
"""

def _rpm_repo_impl(repo_ctx):
    repo_ctx.file("WORKSPACE", "workspace(name = '%s')" % repo_ctx.name)
    rpms = ", \n        ".join(['"%s"' % x for x in repo_ctx.attr.rpms])
    visibility = ", \n        ".join(['"%s"' % x for x in repo_ctx.attr.generated_visibility])
    build_content = __BUILD_FILE_CONTENT__.format(
        repository_name = repo_ctx.attr.bazeldnf,
        rpms = rpms,
        visibility = visibility,
    )
    repo_ctx.file("BUILD.bazel", build_content)

_rpm_repo = repository_rule(
    implementation = _rpm_repo_impl,
    attrs = {
        "rpms": attr.string_list(),
        "bazeldnf": attr.string(),
        "generated_visibility": attr.string_list(),
    },
)

def _handle_lock_file(module_ctx, lock_file, repositories):
    content = module_ctx.read(lock_file.path)
    content = json.decode(content)
    repos = []
    for rpm in content["rpms"]:
        if rpm["name"] not in repositories:
            _rpm_repository(
                name = rpm["name"],
                sha256 = rpm.get("sha256", None),
                integrity = rpm.get("integrity", None),
                urls = rpm.get("urls", []),
            )
            repositories[rpm["name"]] = rpm

        repos.append("@%s//rpm" % rpm["name"])
    _rpm_repo(
        name = lock_file.rpm_tree_name,
        bazeldnf = lock_file.bazeldnf,
        generated_visibility = lock_file.generated_visibility,
        rpms = repos,
    )
    return lock_file.rpm_tree_name, module_ctx.is_dev_dependency(lock_file)

def _rpm_deps_impl(module_ctx):
    public_repos = []
    is_dev_dependency = False
    repositories = dict()

    for module in module_ctx.modules:
        if module.tags.lock_file:
            for lock_file in module.tags.lock_file:
                repo_name, _is_dev_dependency = _handle_lock_file(module_ctx, lock_file, repositories)
                public_repos.append(repo_name)
                is_dev_dependency = is_dev_dependency or _is_dev_dependency

        for rpm in module.tags.rpm:
            is_dev_dependency = is_dev_dependency or module_ctx.is_dev_dependency(rpm)

            _rpm_repository(
                name = rpm.name,
                sha256 = rpm.sha256,
                integrity = rpm.integrity,
                urls = rpm.urls,
                dependencies = rpm.dependencies,
            )
            public_repos.append(rpm.name)

    if not hasattr(module_ctx, "extension_metadata") or is_dev_dependency:
        return None

    return module_ctx.extension_metadata(
        root_module_direct_deps = public_repos,
        root_module_direct_dev_deps = [],
    )

_rpm_tag = tag_class(
    attrs = {
        "name": attr.string(),
        "sha256": attr.string(),
        "urls": attr.string_list(),
        "strip_prefix": attr.string(),
        "integrity": attr.string(),
        "dependencies": attr.label_list(),
    },
)

_lock_file_tag = tag_class(
    attrs = {
        "rpm_tree_name": attr.string(),
        "path": attr.label(),
        "bazeldnf": attr.string(
            default = "bazeldnf",
            doc = "The name of the bazel repository containing the bazeldnf rules",
        ),
        "generated_visibility": attr.string_list(
            default = ["//visibility:public"],
            doc = "The visibility rule for the generated rpmtree",
        ),
    },
)

rpm_deps = module_extension(
    _rpm_deps_impl,
    tag_classes = {
        "rpm": _rpm_tag,
        "lock_file": _lock_file_tag,
    },
)
