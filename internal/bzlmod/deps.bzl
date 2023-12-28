load("@bazel_tools//tools/build_defs/repo:http.bzl", "http_file")
load("//:deps.bzl", "PREBUILD_BINARIES")

def _prebuild_binaries_impl(module_ctx):
    for archive in PREBUILD_BINARIES:
        http_file(executable = True, **archive)

    if not hasattr(module_ctx, "extension_metadata"):
        return None

    return module_ctx.extension_metadata()

prebuild_binaries = module_extension(
    _prebuild_binaries_impl,
)
