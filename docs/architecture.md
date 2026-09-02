# FlowCart OS Architecture

## Current Architecture

FlowCart currently has a small browser-to-API-to-database architecture:

```text
Browser
	-> Next.js / React / TypeScript
	-> HTTP
	-> Go / Chi
	-> pgxpool
	-> PostgreSQL 18.6
```

The frontend is a Next.js application using React, TypeScript, and Tailwind
CSS. It calls the Go HTTP API over HTTP using the URL configured by
`NEXT_PUBLIC_API_URL`.

The backend is a Go application using the Chi router, the standard
`net/http` package, and `pgxpool` from pgx. It creates and pings a PostgreSQL
connection pool during startup. Its current API surface contains one endpoint:

- `GET /health` pings PostgreSQL and returns the API status, service name, and
	database status as JSON. It returns a non-200 response when the database is
	unavailable.

The local frontend runs on `http://localhost:3000`. The local Go API runs on
`http://localhost:8081` because port `8080` is occupied by Oracle TNS Listener
on the current Windows development machine. The backend port remains
configurable through the `PORT` environment variable.

The existing local PostgreSQL installation uses host port `5432`. FlowCart's
Docker PostgreSQL service uses host port `5433`, mapped to container port
`5432`. The database URL is supplied through `DATABASE_URL`.

## Future Phases

Database migrations and application tables, Redis, authentication, inventory,
orders, workers, and AWS infrastructure are future phases. PostgreSQL
infrastructure is current, but those application features are not part of the
running architecture.
