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

{rpm_aliases}
"""

def _rpm_repo_impl(repo_ctx):
    repo_ctx.file("WORKSPACE", "workspace(name = '%s')" % repo_ctx.name)
    rpms = []
    list_of_rpm = []
    rpm_aliases = []
    visibility = ", \n        ".join(['"%s"' % x for x in repo_ctx.attr.generated_visibility])
    for rpm in repo_ctx.attr.rpms:
        rpms.append('"%s"' % rpm)
        alias = "rpm_%s" % rpm.replace("-", "_").replace("@", "").split("//", 1)[0]
        list_of_rpm.append(
            '"@{repo}//:{alias}"'.format(repo = repo_ctx.name.rsplit("~", 1)[-1], alias = alias),
        )
        rpm_aliases.append(
            'alias( name = "{alias}", actual = "{rpm}", visibility = [ {visibility}] )'.format(
                alias = alias,
                rpm = rpm,
                visibility = visibility,
            ),
        )

    rpms = ", \n        ".join(rpms)
    list_of_rpm = ",\n    ".join(list_of_rpm)
    rpm_aliases = "\n".join(rpm_aliases)
    build_content = __BUILD_FILE_CONTENT__.format(
        repository_name = repo_ctx.attr.bazeldnf,
        rpms = rpms,
        visibility = visibility,
        rpm_aliases = rpm_aliases,
    )
    repo_ctx.file("BUILD.bazel", build_content)
    repo_ctx.file("rpms.bzl", "RPMS = [\n    %s\n]\n" % list_of_rpm)

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
    repositories = dict()

    for module in module_ctx.modules:
        for lock_file in module.tags.lock_file:
            _handle_lock_file(module_ctx, lock_file, repositories)

        for rpm in module.tags.rpm:
            if rpm.name not in repositories:
                _rpm_repository(
                    name = rpm.name,
                    sha256 = rpm.sha256,
                    integrity = rpm.integrity,
                    urls = rpm.urls,
                    dependencies = rpm.dependencies,
                )
                repositories[rpm.name] = rpm

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
