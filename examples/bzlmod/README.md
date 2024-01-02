# Example usage of bzlmod for bazeldnf

This example shows how to make use of bzlmod support with lockfiles for bazeldnf

## Instructions

After checkin out the project the first or whenever the yaml configuring the
repositories get modified you need to retrieve the repository
configuration:

```bash
bazel run @//:bazeldnf -- fetch --repofile repo-configs/*.yaml
```

To generate a new lock file you would execute:

```bash
bazel run :bazeldnf -- rpmtree \
    --basesystem centos-release \
    --repofile repo-configs/centos7.yaml \
    --name rpm \
    --bzlmod \
    --lock-file rpmtree/rpm.json rpm
```

In this example we're generating the lock file `rpmtree/centos7/rpm.json`
containing the necessary rpms to get a CentOS7 `rpm` binary running and the
generated rpmtree will be named `@rpm//:rpms`
