load("@bazel_skylib//lib:shell.bzl", "shell")
load("//bazeldnf:toolchain.bzl", "BAZELDNF_TOOLCHAIN")

JQ_TOOLCHAIN = "@aspect_bazel_lib//lib:jq_toolchain_type"

def _update_lock_file_impl(ctx):
    out_file = ctx.actions.declare_file(ctx.label.name + ".bash")

    bazeldnf = ctx.toolchains[BAZELDNF_TOOLCHAIN]
    jq = ctx.toolchains[JQ_TOOLCHAIN].jqinfo.bin

    substitutions = {
        "@@BAZELDNF_SHORT_PATH@@": shell.quote(bazeldnf._tool.short_path),
        "@@JQ_SHORT_PATH@@": shell.quote(jq.short_path),
        "@@LOCK_FILE@@": shell.quote(ctx.attr.lock_file),
    }

    ctx.actions.expand_template(
        template = ctx.file._runner,
        output = out_file,
        substitutions = substitutions,
        is_executable = True,
    )

    runfiles = ctx.runfiles(
        files = [bazeldnf._tool, jq],
    )

    return [DefaultInfo(
        files = depset([out_file]),
        runfiles = runfiles,
        executable = out_file,
    )]

update_lock_file = rule(
    implementation = _update_lock_file_impl,
    attrs = {
        "lock_file": attr.string(),
        "_runner": attr.label(allow_single_file = True, default = Label("//bazeldnf/private:update-lock-file.sh")),
    },
    toolchains = [
        BAZELDNF_TOOLCHAIN,
        JQ_TOOLCHAIN,
    ],
    executable = True,
)
