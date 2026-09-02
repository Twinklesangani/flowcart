# FlowCart OS Development

## Project Root

```text
C:\Users\TWINKLE SANGANI\OneDrive\Desktop\flowcart
```

Open PowerShell at the project root before running the commands below.

## Prerequisites

- Node.js and npm
- Go

No database or other infrastructure services are required for the current
foundation.

## Run the Frontend

```powershell
cd apps\web
npm run dev
```

Frontend URL:

```text
http://localhost:3000
```

The frontend reads the backend URL from `NEXT_PUBLIC_API_URL`. Local
development uses `http://localhost:8081`.

## Run the Backend

In a separate PowerShell window:

```powershell
cd apps\api
$env:PORT="8081"
go run ./cmd/server
```

Backend URL:

```text
http://localhost:8081
```

Port `8080` is currently occupied by Oracle TNS Listener on this Windows
development machine, so the local API uses port `8081`. The backend still
accepts a configurable port through the `PORT` environment variable.

## Health Check

With the backend running, open or request:

```text
http://localhost:8081/health
```

The endpoint is `GET /health` and returns:

```json
{
	"status": "ok",
	"service": "flowcart-api"
}
```
