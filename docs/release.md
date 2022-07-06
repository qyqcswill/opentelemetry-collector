# OpenTelemetry Collector Release Procedure

Collector build and testing is currently fully automated. However there are still certain operations that need to be performed manually in order to make a release.

We release both core and contrib collectors with the same versions where the contrib release uses the core release as a dependency. We’ve divided this process into four sections. A release engineer must release:
1. The [Core](#releasing-opentelemetry-collector) collector, including the collector builder CLI tool.
1. The [Contrib](#releasing-opentelemetry-collector-contrib) collector.
1. The [artifacts](#producing-the-artifacts)

**Important Note:** You’ll need to be able to sign git commits/tags in order to be able to release a collector version. Follow [this guide](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits) to setup it up.

**Important Note:** You’ll need to be an approver for both the repos in order to be able to make the release. This is required as you’ll need to push tags and commits directly to the following repositories:

* [open-telemetry/opentelemetry-collector](https://github.com/open-telemetry/opentelemetry-collector)
* [open-telemetry/opentelemetry-collector-contrib](https://github.com/open-telemetry/opentelemetry-collector-contrib)
* [open-telemetry/opentelemetry-collector-releases](https://github.com/open-telemetry/opentelemetry-collector-releases)

## Release manager

A release manager is the person responsible for a specific release. While the manager might request help from other folks, they are ultimately responsible for the success of a release.

In order to have more people comfortable with the release process, and in order to decrease the burden on a small number of volunteers, all core approvers are release managers from time to time, listed under the [Release Schedule](#release-schedule) section. That table is updated at every release, with the current manager adding themselves to the bottom of the table, removing themselves from the top of the table.

It is possible that a core approver isn't a contrib approver. In that case, the release manager should coordinate with a contrib approver for the steps requiring such role, like the publishing of tags.

## Releasing opentelemetry-collector

1. Make sure that there are no open issues with `release:blocker` label in [Core](https://github.com/open-telemetry/opentelemetry-collector/labels/release%3Ablocker) or [Contrib](https://github.com/open-telemetry/opentelemetry-collector-contrib/labels/release%3Ablocker) repo. The release has to be delayed until they are resolved.

1. Make sure the current `main` branch build successfully passes ([Core](https://github.com/open-telemetry/opentelemetry-collector/actions/workflows/build-and-test.yml?query=branch%3Amain) and [Contrib](https://github.com/open-telemetry/opentelemetry-collector-contrib/actions/workflows/build-and-test.yml?query=branch%3Amain)).

1. Determine the version number that will be assigned to the release. During the beta phase, we increment the minor version number and set the patch number to 0. In this document, we are using `v0.55.0` as the version to be released, following `v0.54.0`.

1. To keep track of the progress, it might be helpful to create a [tracking issue](https://github.com/open-telemetry/opentelemetry-collector/issues/new?assignees=&labels=release&template=release.md&title=Release+vX.X.X) similar to [#4870](https://github.com/open-telemetry/opentelemetry-collector/issues/4870). You are responsible for all of them, except the operator one. Once the issue is created, you can create the individual ones by hovering them and clicking the "Convert to issue" button on the right hand side.

1. Update Contrib to use the latest in development version of Core. Run `make update-otel` in Contrib root directory and if it results in any changes, submit a PR. Open this PR as draft. This is to ensure that the latest core does not break contrib in any way. We’ll update it once more to the final release number later.

1. Prepare Core for release.

    * Update CHANGELOG.md file and rename the Unreleased section to the new release name. Add a new unreleased section at top. Use commit history feature to get the list of commits since the last release to help understand what should be in the release notes, e.g.: https://github.com/open-telemetry/opentelemetry-collector/compare/v0.44.0...main.

    * Use multimod to update the version of the collector package
      * Update [versions.yaml](https://github.com/open-telemetry/opentelemetry-collector/blob/main/versions.yaml) and commit it locally
      * Run `make multimod-prerelease` (it might fail if you didn't commit the change above)

    * Update the collector version in the collector builder to the new release in `./cmd/builder/internal/builder/config.go`.

    * Update the collector version in the manifest used by `make run` to the new release in `./cmd/otelcorecol/builder-config.yaml` and run `make genotelcorecol`.

    * Update the collector version in the `examples/k8s/otel-config.yaml` 

    * Submit a PR with the changes and get the PR approved and merged.

    * Ensure the `main` branch builds successfully.

1. Create a branch named `release/<release-series>` (e.g. `release/v0.45.x`) from the changelog update commit and push to `open-telemetry/opentelemetry-collector`.

1. Tag all the modules with the new release version by running the `make add-tag` command (e.g. `make add-tag TAG=v0.55.0`). Push them to `open-telemetry/opentelemetry-collector` with `make push-tag TAG=v0.55.0`. Wait for the new tag build to pass successfully.

1. The release script for the collector builder should create a new GitHub release for the builder. This is a separate release from the core, but we might join them in the future if it makes sense.

1. A new `v0.55.0` release should be automatically created on Github by now. Edit it and use the contents from the CHANGELOG.md as the release's description. At the top of the release's changelog, add a link to the releases repository where the binaries and other artifacts are landing, like:

```
### Images and binaries here: https://github.com/open-telemetry/opentelemetry-collector-releases/releases/tag/v0.55.0
```

## Releasing opentelemetry-collector-contrib

1. Prepare Contrib for release.

  * Update CHANGELOG.md file, this is now done via `chloggen`. Run the following command from the root of the opentelemetry-collector-contrib repo:
      * `make chlog-update VERSION=v0.55.0`

  * Use multimod to update the version of the collector package:

      * Update [versions.yaml](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/versions.yaml) and commit it

      * Run `make multimod-prerelease`

1. Update the Core dependency to the Core version we just released with `make update-otel OTEL_VERSION=v0.55.0` command. Create a PR with both the changes, get it approved and merged.

1. Create a branch named `release/<release-series>` (e.g. `release/v0.45.x`) in Contrib from the changelog update commit and push it to `open-telemetry/opentelemetry-collector-contrib`.

1. Tag all the modules with the new release version by running the `make add-tag TAG=v0.55.0` command. Push them to `open-telemetry/opentelemetry-collector-contrib` with `make push-tag TAG=v0.55.0`. Wait for the new tag build to pass successfully.

1. A new `v0.55.0` release should be automatically created on Github by now. Edit it and use the contents from the CHANGELOG.md as the release's description. At the top of the description add a link to Core release notes (assuming the previous release of Core and Contrib was also performed simultaneously), e.g. "The OpenTelemetry Collector Contrib contains everything in the [opentelemetry-collector release](https://github.com/open-telemetry/opentelemetry-collector/releases/tag/v0.55.0) (be sure to check the release notes here as well!)."

## Producing the artifacts

The last step of the release process creates artifacts for the new version of the collector and publishes images to Dockerhub. The steps in this portion of the release are done in the [opentelemetry-collector-releases](https://github.com/open-telemetry/opentelemetry-collector-releases) repo.

1. Update the `./distribution/**/manifest.yaml` files to include the new release version.

1. Update the builder version in `OTELCOL_BUILDER_VERSION` to the new release in the `Makefile`. While this might not be strictly necessary for every release, this is a good practice.

1. Create a pull request with the change and ensure the build completes successfully. See [example](https://github.com/open-telemetry/opentelemetry-collector-releases/pull/71).

1. Tag with the new release version by running the `make add-tag TAG=v0.55.0` command. Push them to `open-telemetry/opentelemetry-collector-releases` with `make push-tag TAG=v0.55.0`. Wait for the new tag build to pass successfully.

1. Ensure the "Release" action passes, this will

    1. push new container images to https://hub.docker.com/repository/docker/otel/opentelemetry-collector
    
    1. create a Github release for the tag and push all the build artifacts to the Github release. See [example](https://github.com/open-telemetry/opentelemetry-collector-releases/actions/runs/1346637081).

## Troubleshooting

1. `unknown revision internal/coreinternal/v0.55.0` -- This is typically an indication that there's a dependency on a new module. You can fix it by adding a new `replaces` entry to the `go.mod` for the affected module. 

## Release schedule

| Date       | Version | Release manager |
| ---------- | ------- | --------------- |
| 2022-06-22 | v0.54.0 | @Aneurysm9      |
| 2022-07-06 | v0.55.0 | @tigrannajaryan |
| 2022-07-20 | v0.56.0 | @dmitryax       |
| 2022-08-03 | v0.57.0 | @jpkrohling     |
| 2022-08-17 | v0.58.0 | @bogdandrutu    |
| 2022-08-31 | v0.59.0 | @codeboten      |
| 2022-09-14 | v0.60.0 | @mx-psi         |
