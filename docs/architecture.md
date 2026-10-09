# Architecture

FURII starts as a modular monolith: a React/Vite/TypeScript client, one Go `net/http` API, and PostgreSQL. PostgreSQL is the source of truth. Domain services own business rules and remain independent of HTTP request types. The initial deployment uses Docker Compose; production requires HTTPS, private database access, protected backups, and managed secrets.

The initial API uses REST/JSON under `/api/v1`. Authentication is server-side and revocable: clients receive an opaque random session token in an HttpOnly cookie; PostgreSQL stores only its keyed SHA-256 digest. Passwords use Argon2id. School and user records carry UUID identifiers and school scope.

## Architecture decision record 001: foundation

- HTTP: Go standard `net/http` for a small dependency surface and explicit middleware.
- Database: PostgreSQL, accessed with `pgx`; Goose versioned SQL migrations.
- Authentication: opaque cookie sessions with server-side revocation and Argon2id password hashes.
- API: REST/JSON, `/api/v1`, consistent `{data: ...}` and `{error: ...}` envelopes.
- Deployment: Docker Compose for local development; production topology is deployment-specific.
- Frontend: React + Vite + TypeScript; API client and route structure start minimal and grow by domain.

These choices are intentionally revisable as the pilot supplies evidence. No Redis, microservices, or offline exam claims are part of the foundation.
