# FlowCart OS Architecture

## Current Architecture

FlowCart currently has a small browser-to-API architecture:

```text
Browser
	-> Next.js / React / TypeScript
	-> HTTP
	-> Go / Chi
	-> GET /health
```

The frontend is a Next.js application using React, TypeScript, and Tailwind
CSS. It calls the Go HTTP API over HTTP using the URL configured by
`NEXT_PUBLIC_API_URL`.

The backend is a Go application using the Chi router and the standard
`net/http` package. Its current API surface contains one endpoint:

- `GET /health` returns the API status and service name as JSON.

The local frontend runs on `http://localhost:3000`. The local Go API runs on
`http://localhost:8081` because port `8080` is occupied by Oracle TNS Listener
on the current Windows development machine. The backend port remains
configurable through the `PORT` environment variable.

## Future Phases

PostgreSQL, Redis, authentication, inventory, orders, workers, and AWS
infrastructure are future phases. They are not part of the current running
architecture.
