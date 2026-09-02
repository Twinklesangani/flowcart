# FlowCart OS Architecture Decisions

## Current Decisions

- Use React through Next.js for the frontend.
- Use TypeScript for frontend application code.
- Use Go for the backend.
- Use Chi as the Go HTTP router.
- Use PostgreSQL as the primary database.
- Use `pgxpool` from pgx for Go database connection management.
- Use a modular monolith architecture.
- Use port `8081` for the local Go API because port `8080` conflicts with
	Oracle TNS Listener on the development machine.
- Keep the production backend port configurable through the `PORT`
	environment variable.
- Configure the frontend-to-backend URL through `NEXT_PUBLIC_API_URL`.

## Planned Decisions

- Future backend features will generally follow the
	`handler -> service -> repository` architecture.

PostgreSQL infrastructure is implemented, but migrations and application
tables are not implemented in the current foundation.
