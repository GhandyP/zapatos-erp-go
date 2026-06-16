# ci-cd-foundation Specification

## Purpose

Define the repository automation that validates changes before merge and produces traceable release outputs.

## Requirements

### Requirement: Pull request validation is mandatory

The repository MUST run automated validation on pull requests and protected-branch updates.

#### Scenario: Valid change passes validation

- GIVEN a change is opened for review
- WHEN automated validation runs
- THEN the repository MUST execute its test and build checks
- AND the change MUST be eligible for merge only if those checks pass

#### Scenario: Failing check blocks progress

- GIVEN a test or build check fails
- WHEN validation completes
- THEN the change MUST be marked failed
- AND it MUST not be treated as merge-ready

### Requirement: Releases are gated and traceable

The repository MUST only produce release outputs from a validated revision and the output MUST be traceable to that revision.

#### Scenario: Tagged release is produced from passing revision

- GIVEN the revision has passed validation
- WHEN a release is created from that revision
- THEN the repository MUST produce a versioned release output
- AND the output MUST be attributable to the same revision

#### Scenario: Unvalidated revision cannot release

- GIVEN validation has not passed
- WHEN a release is requested
- THEN the repository MUST reject the release
- AND it MUST not publish an output
