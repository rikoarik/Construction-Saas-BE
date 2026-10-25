# Project Permission Engine (BE-014)

BE-014 adds the proposed `projects` and `project_memberships` storage foundation and an explicit default-deny `projects.Authorizer`. A permission is allowed only when the authenticated user has active company membership, active membership in that exact project, the project/company pair is consistent, and the caller-supplied permission policy explicitly grants that permission to the stored project role. Company roles do not implicitly confer project permissions; unknown roles, unknown permissions, missing memberships, suspended memberships, mismatched company/project IDs, and inactive projects are denied.

The canonical documentation defines operation permission keys but not project role values or the role-to-permission matrix. Therefore the engine accepts an explicit `GrantPolicy` from its caller; no product-wide role grants are silently hard-coded. API handlers for future project CRUD tasks must call the authorizer at the project/application boundary before mutations or reads. This foundation does not implement project CRUD, project team management, support elevation, or endpoint bindings yet.

Database tests use a dedicated PostgreSQL database via `TEST_DATABASE_URL`:

```sh
TEST_DATABASE_URL='postgres://app:app@localhost:54329/construction_saas_test?sslmode=disable' \
  go test ./internal/projects -count=1
```
