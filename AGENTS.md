# FlowCart OS — AI Development Instructions

## Project

FlowCart OS is a production-style multi-tenant, multi-warehouse order,
inventory and fulfilment management platform.

It is being built as a flagship portfolio project for full-stack and
software-development internship applications.

## Main Stack

Frontend:

- Next.js
- React
- TypeScript
- Tailwind CSS

Backend:

- Go
- Chi
- pgx
- PostgreSQL

Infrastructure:

- Redis
- Asynq
- Docker
- Docker Compose
- GitHub Actions
- AWS eventually

API:

- REST
- OpenAPI / Swagger

Architecture:

- Modular monolith

Backend flow:

Handler -> Service -> Repository -> PostgreSQL

## Important Future Features

- Multi-tenancy
- Role-based access control
- Products
- Warehouses
- Inventory management
- Inventory reservations
- Concurrency-safe stock operations
- Multi-warehouse order allocation
- Orders
- Payments
- Idempotent webhook handling
- Background jobs
- Retries
- Real-time updates
- Inventory transfers
- Notifications
- Audit logs
- Analytics
- Automated testing
- CI/CD
- Cloud deployment

## Development Rules

1. Do not build the entire application at once.

2. Only implement the feature requested in the current task.

3. Inspect the existing repository before making major changes.

4. Backend should generally follow:

Handler -> Service -> Repository -> Database

5. Do not place complicated business logic inside HTTP handlers.

6. PostgreSQL is the primary database.

7. Inventory-related operations requiring consistency must use proper
   PostgreSQL transactions.

8. Inventory must never become negative because of concurrent requests.

9. Organization-owned data must remain isolated between organizations.

10. Significant backend features should have tests.

11. Do not introduce unnecessary dependencies.

12. Do not introduce microservices unless specifically requested.

13. Do not introduce Kubernetes unless specifically requested.

14. Never commit secrets or real credentials.

15. Use .env.example to document environment variables.

16. Never remove existing working functionality without a valid reason.

17. Follow existing project patterns.

18. Prefer readable solutions over unnecessary abstraction.

19. Explain important architectural decisions.

20. After implementing a task:

- format code
- run linting where applicable
- run tests
- run builds
- report failures
- fix failures caused by the changes

## Learning Requirement

The developer is learning while building.

After important implementations, explain:

- what was created
- why it exists
- how the pieces communicate
- important concepts introduced
- important files
- how to run it
- how to test it

Do not assume advanced Go, React, PostgreSQL, or distributed-systems knowledge.

## OpenOMS

OpenOMS can be studied for architectural inspiration.

Do NOT copy OpenOMS source code directly into FlowCart.

FlowCart must remain independently implemented.

## Development Philosophy

Build -> Run -> Test -> Understand -> Commit
