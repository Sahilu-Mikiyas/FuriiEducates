# Database migrations

Schema changes live in `services/api/migrations` as numbered Goose files. Migrations `00004` to `00009` (staff invitations, assessments, learner accounts, grading, learning intelligence, and assessment recovery) were originally applied by hand through the Supabase SQL editor. That is no longer necessary.

## How migrations run now

The API applies every pending migration when it starts (`services/api/migrations/runner.go`):

- An advisory lock ensures only one API instance migrates at a time.
- Each migration runs in its own transaction and is recorded in `goose_db_version`.
- If a migration fails, the API logs the error and exits instead of serving requests against a half-migrated schema.
- Databases that received `00004` to `00010` through Supabase's migration history are recognised and baselined once, after their schema and security settings are verified.

To deploy a schema change, merge it and let Render redeploy the API. Then check the Render log for `database migration failed`. Take a Supabase restore point first for any migration that rewrites or deletes data.

## Manual fallback

From `services/api`, in a trusted shell that already has the production `DATABASE_URL` set:

```sh
go run github.com/pressly/goose/v3/cmd/goose@v3.24.1 -dir migrations postgres "$DATABASE_URL" up
```

Do not paste SQL into the Supabase editor, put the connection string in a committed file, or share it in a chat.
