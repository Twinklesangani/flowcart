# FlowCart OS Frontend

The FlowCart frontend is a Next.js 16 operations workspace for dashboards,
orders, inventory, replenishment, transfers, timelines, and organization audit
history.

## Requirements

- Node.js and npm
- A running FlowCart API
- `NEXT_PUBLIC_API_URL` pointing to that API

Create `apps/web/.env.local` from `.env.example`:

```text
NEXT_PUBLIC_API_URL=http://localhost:8081
```

`NEXT_PUBLIC_` variables are public. Do not put secrets in them.

## Commands

```powershell
npm ci
npm run dev
npm run lint
npx tsc --noEmit
npm run build
```

The development frontend runs at `http://localhost:3000`.

## Authentication

Access tokens stay in React memory and are never stored in localStorage or
sessionStorage. Refresh uses the API's HttpOnly `flowcart_refresh` cookie with
credentialed requests. The typed API client performs one refresh-and-retry
attempt after an expired access token.