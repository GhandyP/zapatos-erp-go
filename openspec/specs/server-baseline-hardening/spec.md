# server-baseline-hardening Specification

## Purpose

Define baseline HTTP protections for a safer default server.

## Requirements

### Requirement: Conservative server limits

The HTTP server MUST enforce conservative timeouts and reject oversized request bodies.

#### Scenario: Normal request completes

- GIVEN a valid request within normal size and time bounds
- WHEN it reaches the server
- THEN the request MUST complete successfully
- AND it MUST not be blocked by baseline limits

#### Scenario: Oversized request is rejected

- GIVEN a request body larger than the configured limit
- WHEN the server receives it
- THEN the server MUST reject the request
- AND it MUST not process the full body

### Requirement: Safe failures and security headers

The system MUST recover from handler panics and include baseline security headers on successful responses.

#### Scenario: Handler panic is contained

- GIVEN a handler panics while processing a request
- WHEN the request is served
- THEN the server MUST return a safe failure response
- AND it MUST remain available for later requests

#### Scenario: Successful response includes headers

- GIVEN a normal successful response
- WHEN the response is returned
- THEN it MUST include baseline security headers
- AND it MUST not expose unnecessary server details
