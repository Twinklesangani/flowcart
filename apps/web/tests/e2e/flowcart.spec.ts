import { expect, test, type Page } from "@playwright/test";
import pg from "pg";

const apiURL = process.env.API_URL ?? (process.env.API_MODE === "proxy" ? "" : "http://127.0.0.1:8081");
const ownerEmail = "owner@flowcart.demo";
const viewerEmail = "viewer@flowcart.demo";
const password = "FlowCart-Demo-Only-2026!";

type APIResult<T> = { status: number; body: T };

async function login(page: Page, email: string) {
  await page.goto("/");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.locator("form").getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/dashboard/);
}

async function api<T>(page: Page, token: string, path: string, options: RequestInit = {}): Promise<APIResult<T>> {
  return page.evaluate(async ({ apiURL, token, path, options }) => {
    const response = await fetch(`${apiURL}${path}`, {
      ...options,
      credentials: "include",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}`, ...(options.headers ?? {}) },
    });
    let body: unknown = null;
    try { body = await response.json(); } catch { /* 204 */ }
    return { status: response.status, body } as APIResult<T>;
  }, { apiURL, token, path, options });
}

async function ownerContext(page: Page) {
  const responsePromise = page.waitForResponse((response) => response.url().endsWith("/api/v1/auth/login"));
  await page.goto("/");
  await page.getByLabel("Email").fill(ownerEmail);
  await page.getByLabel("Password").fill(password);
  await page.locator("form").getByRole("button", { name: "Sign in" }).click();
  const loginBody = await (await responsePromise).json() as { access_token: string };
  await expect(page).toHaveURL(/dashboard/);
  return loginBody.access_token;
}

test("owner workflow, denied viewer action, and logout survive a disposable seeded environment", async ({ browser, page }) => {
  const token = await ownerContext(page);
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();

  const organizations = await api<{ organizations: { id: string }[] }>(page, token, "/api/v1/organizations");
  expect(organizations.status).toBe(200);
  const organizationID = organizations.body.organizations[0].id;
  const inventory = await api<{ inventory: { id: string; product_id: string; available_quantity: number }[] }>(page, token, `/api/v1/organizations/${organizationID}/inventory?limit=50`);
  expect(inventory.status).toBe(200);
  const source = inventory.body.inventory.find((item) => item.available_quantity > 0) ?? inventory.body.inventory[0];
  expect(source?.id).toBeTruthy();

  await page.goto("/orders");
  await expect(page.getByRole("heading", { name: "Orders" })).toBeVisible();
  const createResponse = page.waitForResponse((response) => response.url().endsWith(`/api/v1/organizations/${organizationID}/orders`) && response.request().method() === "POST");
  await page.getByRole("button", { name: "Create order", exact: true }).click();
  const orderResponse = await createResponse;
  expect(orderResponse.status()).toBe(201);
  const order = await orderResponse.json() as { id: string; items: { reservation_id?: string; reservation_status?: string }[] };
  expect(order.items[0]?.reservation_id).toBeTruthy();
  expect(order.items[0]?.reservation_status).toBe("active");
  const orderRow = page.locator("tbody tr").filter({ hasText: order.id.slice(0, 8) });
  await expect(orderRow).toHaveCount(1);
  await expect(orderRow.locator(`a[href="/orders/${order.id}"]`)).toBeVisible();
  const persistedOrder = await api<{ id: string; items: { reservation_id?: string; reservation_status?: string }[] }>(page, token, `/api/v1/organizations/${organizationID}/orders/${order.id}`);
  expect(persistedOrder.status).toBe(200);
  expect(persistedOrder.body.items[0]?.reservation_id).toBe(order.items[0]?.reservation_id);
  expect(persistedOrder.body.items[0]?.reservation_status).toBe("active");
  const databaseURL = process.env.DATABASE_URL;
  expect(databaseURL).toBeTruthy();
  const database = new pg.Client({ connectionString: databaseURL });
  await database.connect();
  try {
    const databaseOrder = await database.query<{ order_id: string; reservation_id: string; reservation_status: string; quantity: number }>("SELECT o.id AS order_id, r.id AS reservation_id, r.status AS reservation_status, r.quantity FROM orders o JOIN order_items oi ON oi.order_id = o.id JOIN inventory_reservations r ON r.order_item_id = oi.id WHERE o.id = $1", [order.id]);
    expect(databaseOrder.rows).toHaveLength(1);
    expect(databaseOrder.rows[0]).toMatchObject({ order_id: order.id, reservation_id: order.items[0]?.reservation_id, reservation_status: "active" });
    expect(Number(databaseOrder.rows[0].quantity)).toBe(1);
  } finally {
    await database.end();
  }

  const warehouses = await api<{ warehouses: { id: string }[] }>(page, token, `/api/v1/organizations/${organizationID}/warehouses?limit=50`);
  expect(warehouses.status).toBe(200);
  expect(warehouses.body.warehouses.length).toBeGreaterThanOrEqual(2);
  const transfer = await api<{ id: string }>(page, token, `/api/v1/organizations/${organizationID}/transfers`, {
    method: "POST",
    headers: { "Idempotency-Key": `e2e-transfer-${Date.now()}` },
    body: JSON.stringify({ source_warehouse_id: warehouses.body.warehouses[0].id, destination_warehouse_id: warehouses.body.warehouses[1].id, items: [{ product_id: source.product_id, quantity: 1 }] }),
  });
  expect(transfer.status).toBe(201);
  const dispatched = await api(page, token, `/api/v1/organizations/${organizationID}/transfers/${transfer.body.id}/dispatch`, { method: "POST", headers: { "Idempotency-Key": `e2e-dispatch-${Date.now()}` } });
  expect(dispatched.status).toBe(200);
  const received = await api(page, token, `/api/v1/organizations/${organizationID}/transfers/${transfer.body.id}/receive`, { method: "POST", headers: { "Idempotency-Key": `e2e-receive-${Date.now()}` } });
  expect(received.status).toBe(200);
  await page.goto(`/transfers/${transfer.body.id}`);
  await expect(page.getByText("Transfer detail", { exact: true })).toBeVisible();
  await expect(page.getByText("completed", { exact: true })).toBeVisible();

  const viewer = await browser.newPage();
  await login(viewer, viewerEmail);
  await viewer.goto(`/transfers/${transfer.body.id}`);
  await expect(viewer.getByText("Transfer detail", { exact: true })).toBeVisible();
  await expect(viewer.getByRole("button", { name: "dispatch", exact: true })).toHaveCount(0);
  await viewer.close();

  await page.getByRole("button", { name: "Sign out" }).click();
  await page.reload();
  await expect(page.getByRole("heading", { name: "Welcome back" })).toBeVisible();
});
