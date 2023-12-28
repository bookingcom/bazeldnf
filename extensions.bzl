load("//internal/bzlmod:deps.bzl", _prebuild_binaries = "prebuild_binaries")
load("//internal/bzlmod:rpm.bzl", _rpm_deps = "rpm_deps")

prebuild_binaries = _prebuild_binaries
rpm_deps = _rpm_deps
