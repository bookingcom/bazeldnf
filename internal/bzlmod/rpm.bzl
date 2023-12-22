load("//:deps.bzl", _rpm_repository = "rpm")

def _rpm_deps_impl(module_ctx):

    for module in module_ctx.modules:
        for rpm in module.tags.rpm:
            _rpm_repository(
                name = rpm.name,
                sha256 = rpm.sha256,
                urls = rpm.urls
            )

    if not hasattr(module_ctx, "extension_metadata"):
        return None

    return module_ctx.extension_metadata()

_rpm_tag = tag_class(
    attrs = {
        "name": attr.string(),
        "sha256": attr.string(),
        "urls": attr.string_list(),
        "strip_prefix": attr.string(),
        "integrity": attr.string()
    }
)

rpm_deps = module_extension(
    _rpm_deps_impl,
    tag_classes = {
        "rpm": _rpm_tag,
    }
)
