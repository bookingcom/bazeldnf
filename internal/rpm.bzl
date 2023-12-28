# Copyright 2014 The Bazel Authors. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

load("@bazel_tools//tools/build_defs/repo:utils.bzl", "update_attrs")

RpmInfo = provider(
    fields = {
        "dependencies": "depset of dependencies"
    }
)

def _rpm_rule_impl(ctx):
    out = RpmInfo(dependencies = depset(ctx.attr.dependencies))

    return [
        DefaultInfo(files = depset([ctx.file.rpm_file])),
        out
    ]

rpm_rule = rule(
    implementation = _rpm_rule_impl,
    attrs = {
        "dependencies": attr.label_list(providers = [RpmInfo]),
        "rpm_file": attr.label(allow_single_file = True)
    }
)

_HTTP_FILE_BUILD = """
load("@{repository_name}//internal:rpm.bzl", "rpm_rule")

package(default_visibility = ["//visibility:public"])
filegroup(
    name = "rpm",
    srcs = ["{downloaded_file_path}"],
)

rpm_rule(
    name = "entry",
    rpm_file = "{downloaded_file_path}",
    dependencies = []
)
"""

def _rpm_impl(ctx):
    if ctx.attr.urls:
        downloaded_file_path = "downloaded"
        download_info = ctx.download(
            url = ctx.attr.urls,
            output = "rpm/" + downloaded_file_path,
            sha256 = ctx.attr.sha256,
            integrity = ctx.attr.integrity,
        )
    else:
        fail("urls must be specified")
    ctx.file("WORKSPACE", "workspace(name = \"{name}\")".format(name = ctx.name))
    build_content = _HTTP_FILE_BUILD.format(
        downloaded_file_path = downloaded_file_path,
        repository_name = ctx.attr.bazeldnf
    )
    ctx.file("rpm/BUILD", build_content)
    return update_attrs(ctx.attr, _rpm_attrs.keys(), {"sha256": download_info.sha256})

_rpm_attrs = {
    "urls": attr.string_list(),
    "strip_prefix": attr.string(),
    "sha256": attr.string(),
    "integrity": attr.string(),
    "dependencies": attr.label_list(),
    "bazeldnf": attr.string(default="bazeldnf"),
}

rpm = repository_rule(
    implementation = _rpm_impl,
    attrs = _rpm_attrs,

)
