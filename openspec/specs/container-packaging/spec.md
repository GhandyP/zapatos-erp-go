# container-packaging Specification

## Purpose

Define the container runtime contract for building and running the application outside a dev shell.

## Requirements

### Requirement: Container builds are reproducible and bootable

The packaged image MUST start the application successfully from the repository artifact without extra host dependencies.

#### Scenario: Fresh image starts the app

- GIVEN the image is built from the repository source
- WHEN the container starts with a writable data directory
- THEN the application MUST boot successfully
- AND it MUST serve the same HTTP behavior as the local runtime

#### Scenario: Missing optional config still boots

- GIVEN no custom runtime configuration is mounted
- WHEN the container starts
- THEN the application MUST use documented defaults
- AND it MUST remain reachable on its configured bind address

### Requirement: Runtime state remains file-backed

The container MUST preserve file-backed application state within the configured data directory across restarts.

#### Scenario: State survives container restart

- GIVEN the application has written data into the configured data directory
- WHEN the container is restarted
- THEN the previously written state MUST still be available
- AND startup MUST not require manual repair

#### Scenario: Data directory is not writable

- GIVEN the configured data directory cannot be written
- WHEN the container starts
- THEN the application MUST fail fast
- AND it MUST report a clear runtime error
