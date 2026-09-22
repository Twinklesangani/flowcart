"use client";

import Link from "next/link";
import { FormEvent, useCallback, useEffect, useRef, useState } from "react";
import { AuditEvent, DashboardMetrics, InventoryLevel, LowStockItem, Order, Payment, Fulfillment } from "../lib/api";
import { canOperate, useSession } from "../lib/session";
import { inventoryStatus, replenishmentTransferHref } from "../lib/inventory-view";
import { appendUnique } from "../lib/pagination-state";
import { EmptyState, ErrorState, LoadingState, PermissionDeniedState } from "./states";
import { InventorySections } from "./inventory-sections";

const dateTime = new Intl.DateTimeFormat("en-AU", { dateStyle: "medium", timeStyle: "short" });
const eventLabels: Record<string, string> = { "order.created": "Order created", "payment.created": "Payment created", "payment.succeeded": "Payment succeeded", "order.paid": "Order paid", "fulfillment.completed": "Fulfilment completed", "order.fulfilled": "Order fulfilled" };

export function StatusBadge({ status }: { status: string }) {
  const label = status.replaceAll("_", " ");
  return <span className={`status-badge status-${status}`}><span className="status-dot" aria-hidden="true" />{label}</span>;
}

function formatMoney(minor: number | null | undefined, currency = "AUD") {
  if (minor === null || minor === undefined) return "Not priced";
  return new Intl.NumberFormat("en-AU", { style: "currency", currency }).format(minor / 100);
}

type WarehouseDisplayRow = { warehouse_id: string; warehouse_name: string; available_quantity: number };
function warehouseDisplayRows(warehouses: DashboardMetrics["warehouses"]): WarehouseDisplayRow[] {
  return warehouses.flatMap((warehouse) => Array.from({ length: warehouse.inventory_count }, (_, index) => ({ warehouse_id: warehouse.id, warehouse_name: warehouse.name, available_quantity: index === 0 ? warehouse.available_quantity : 0 })));
}

function apiStatus(error: unknown) {
  return (error as { apiError?: { status?: number } })?.apiError?.status;
}

function eventLabel(event: string) { return eventLabels[event] ?? event.replaceAll("_", " "); }
function eventSummary(metadata: Record<string, unknown>) { return Object.entries(metadata).filter(([key]) => !/token|secret|hash|payload|body/i.test(key)).slice(0, 3).map(([key, value]) => `${key.replaceAll("_", " ")}: ${String(value)}`).join(" · "); }
function OrderTimeline({ events }: { events: AuditEvent[] }) { return events.length ? <ol className="timeline">{events.map((event) => <li className="timeline-item" key={event.id}><span className="timeline-marker" aria-hidden="true" /><div><div className="timeline-top"><strong>{eventLabel(event.event_type)}</strong><time dateTime={event.created_at}>{dateTime.format(new Date(event.created_at))}</time></div><span className="timeline-actor">{event.actor_type}{event.actor_user_id ? ` · ${event.actor_user_id.slice(0, 8)}` : ""}</span>{eventSummary(event.metadata) && <span className="timeline-meta">{eventSummary(event.metadata)}</span>}</div></li>)}</ol> : <EmptyState title="No timeline events" message="No operational events have been recorded for this order." />; }

function PageHeading({ eyebrow, title, description }: { eyebrow: string; title: string; description: string }) {
  return <div className="page-heading"><div><p className="eyebrow">{eyebrow}</p><h1>{title}</h1><p className="page-description">{description}</p></div></div>;
}

function MetricCard({ label, value, detail, tone = "teal" }: { label: string; value: string | number; detail: string; tone?: string }) {
  return <article className={`metric-card metric-${tone}`}><span>{label}</span><strong>{value}</strong><small>{detail}</small></article>;
}

export function DashboardView() {
  const { client, organization } = useSession();
  const [orders, setOrders] = useState<Order[]>([]);
  const [inventory] = useState<InventoryLevel[]>([]);
  const [lowStockRows, setLowStockRows] = useState<LowStockItem[]>([]);
  const [metrics, setMetrics] = useState<DashboardMetrics | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<unknown>(null);

  const load = useCallback(async () => {
    if (!organization) return;
    setState("loading");
    try {
      const [orderResult, lowStockResult, metricsResult] = await Promise.all([
        client.request<import("../lib/api").OrderPage>(`/api/v1/organizations/${organization.id}/orders?limit=50`),
        client.request<{ inventory: LowStockItem[] }>(`/api/v1/organizations/${organization.id}/inventory/low-stock?limit=100`),
        client.request<DashboardMetrics>(`/api/v1/organizations/${organization.id}/dashboard`),
      ]);
      setOrders(orderResult.orders);
      setLowStockRows(lowStockResult.inventory);
      setMetrics(metricsResult);
      setState("ready");
    } catch (loadError) {
      setError(loadError);
      setState("error");
    }
  }, [client, organization]);

  useEffect(() => { const timer = window.setTimeout(() => { void load(); }, 0); return () => window.clearTimeout(timer); }, [load]);

  if (state === "loading") return <><PageHeading eyebrow="Overview" title="Dashboard" description="A clear read on the work moving through your organization." /><LoadingState label="Loading operational overview" /></>;
  if (state === "error") return <><PageHeading eyebrow="Overview" title="Dashboard" description="A clear read on the work moving through your organization." />{apiStatus(error) === 403 ? <PermissionDeniedState /> : <ErrorState onRetry={() => void load()} />}</>;
  if (metrics) return <DashboardAggregateView metrics={metrics} orders={orders} lowStock={lowStockRows} inventory={warehouseDisplayRows(metrics.warehouses)} />;

  const lowStock = lowStockRows;
  const openOrders = orders.filter((order) => order.status === "pending").length;
  const paidOrders = orders.filter((order) => order.status === "paid").length;
  const outOfStock = lowStock.filter((item) => item.status === "out_of_stock").length;
  const warehouseCount = new Set(inventory.map((item) => item.warehouse_id)).size;

  return <><PageHeading eyebrow="Overview" title="Dashboard" description="A clear read on the work moving through your organization." /><section className="metric-grid" aria-label="Operational metrics"><MetricCard label="Open orders" value={openOrders} detail={`${orders.length} orders in the current view`} /><MetricCard label="Paid orders" value={paidOrders} detail="Ready for fulfilment" tone="green" /><MetricCard label="Low stock" value={lowStock.length} detail="At or below policy" tone="amber" /><MetricCard label="Out of stock" value={outOfStock} detail="Requires attention" tone="red" /></section><div className="dashboard-grid"><section className="panel panel-wide"><div className="panel-heading"><div><p className="eyebrow">Order flow</p><h2>Recent orders</h2></div><Link href="/orders" className="text-button">View all</Link></div>{orders.length === 0 ? <EmptyState title="No orders yet" message="Orders will appear here once your organization starts processing work." /> : <div className="compact-list">{orders.slice(0, 5).map((order) => <Link className="list-row" href={`/orders/${order.id}`} key={order.id}><div><strong>#{order.id.slice(0, 8)}</strong><span>{dateTime.format(new Date(order.created_at))}</span></div><div className="list-row-end"><StatusBadge status={order.status} /><strong>{formatMoney(order.subtotal_minor, order.currency_code ?? "AUD")}</strong></div></Link>)}</div>}</section><section className="panel"><div className="panel-heading"><div><p className="eyebrow">Inventory signal</p><h2>Replenishment</h2></div><Link href="/inventory" className="text-button">Open inventory</Link></div>{lowStock.length === 0 ? <EmptyState title="All inventory is healthy" message="Everything is above its reorder point." /> : <div className="compact-list">{lowStock.slice(0, 5).map((item) => <div className="list-row" key={item.id}><div><strong>{item.product_name}</strong><span>{item.warehouse_name} · {item.available_quantity} available</span></div><StatusBadge status={item.status} /></div>)}</div>}</section><section className="panel"><div className="panel-heading"><div><p className="eyebrow">Warehouse network</p><h2>Inventory overview</h2></div></div>{inventory.length === 0 ? <EmptyState title="No inventory configured" message="Inventory levels will appear once products are assigned to warehouses." /> : <div className="warehouse-list">{Array.from(new Set(inventory.map((item) => item.warehouse_id))).map((warehouseID) => { const rows = inventory.filter((item) => item.warehouse_id === warehouseID); const available = rows.reduce((total, item) => total + item.available_quantity, 0); return <div className="warehouse-row" key={warehouseID}><div><strong>{rows[0].warehouse_name}</strong><span>{rows.length} inventory levels</span></div><strong>{available} available</strong></div>; })}</div>}<p className="panel-footnote">{warehouseCount} warehouse{warehouseCount === 1 ? "" : "s"} represented in the current inventory view.</p></section></div></>;
}

function DashboardAggregateView({ metrics, orders, lowStock, inventory }: { metrics: DashboardMetrics; orders: Order[]; lowStock: LowStockItem[]; inventory: WarehouseDisplayRow[] }) {
  const warehouseIDs = Array.from(new Set(inventory.map((item) => item.warehouse_id)));
  return <><PageHeading eyebrow="Overview" title="Dashboard" description="A clear read on the work moving through your organization." /><section className="metric-grid" aria-label="Operational metrics"><MetricCard label="Open orders" value={metrics.open_order_count} detail={`${metrics.order_count} total orders`} /><MetricCard label="Paid orders" value={metrics.paid_order_count} detail="Ready for fulfilment" tone="green" /><MetricCard label="Low stock" value={metrics.low_stock_count} detail="At or below policy" tone="amber" /><MetricCard label="Out of stock" value={metrics.out_of_stock_count} detail="Requires attention" tone="red" /></section><div className="dashboard-grid"><section className="panel panel-wide"><div className="panel-heading"><div><p className="eyebrow">Order flow</p><h2>Recent orders</h2></div><Link href="/orders" className="text-button">View all</Link></div>{orders.length === 0 ? <EmptyState title="No orders yet" message="Orders will appear here once your organization starts processing work." /> : <div className="compact-list">{orders.slice(0, 5).map((order) => <Link className="list-row" href={`/orders/${order.id}`} key={order.id}><div><strong>#{order.id.slice(0, 8)}</strong><span>{dateTime.format(new Date(order.created_at))}</span></div><div className="list-row-end"><StatusBadge status={order.status} /><strong>{formatMoney(order.subtotal_minor, order.currency_code ?? "AUD")}</strong></div></Link>)}</div>}</section><section className="panel"><div className="panel-heading"><div><p className="eyebrow">Inventory signal</p><h2>Replenishment</h2></div><Link href="/inventory" className="text-button">Open inventory</Link></div>{lowStock.length === 0 ? <EmptyState title="All inventory is healthy" message="Everything is above its reorder point." /> : <div className="compact-list">{lowStock.slice(0, 5).map((item) => <div className="list-row" key={item.id}><div><strong>{item.product_name}</strong><span>{item.warehouse_name} · {item.available_quantity} available</span></div><StatusBadge status={item.status} /></div>)}</div>}</section></div><section className="panel"><div className="panel-heading"><div><p className="eyebrow">Warehouse network</p><h2>Inventory overview</h2></div></div><div className="warehouse-list">{warehouseIDs.map((warehouseID) => { const rows = inventory.filter((item) => item.warehouse_id === warehouseID); return <div className="warehouse-row" key={warehouseID}><div><strong>{rows[0]?.warehouse_name}</strong><span>{rows.length} inventory levels</span></div><strong>{rows.reduce((total, item) => total + item.available_quantity, 0)} available</strong></div>; })}</div><p className="panel-footnote">{metrics.warehouse_count} warehouses represented in the organization.</p></section></>;
}

export function OrdersView() {
  const { client, organization } = useSession();
  const [orders, setOrders] = useState<Order[]>([]);
  const [inventory, setInventory] = useState<InventoryLevel[]>([]);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("all");
  const [selectedInventoryID, setSelectedInventoryID] = useState("");
  const [quantity, setQuantity] = useState("1");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState("");
  const [createdOrderID, setCreatedOrderID] = useState("");
  const [nextCursor, setNextCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<unknown>(null);
  const generation = useRef(0);
  const loadingMoreRef = useRef(false);
  const pageRef = useRef({ nextCursor: "", hasMore: false });

  const load = useCallback(async (cursor = "", append = false, requestGeneration = generation.current) => {
    if (!organization || requestGeneration !== generation.current) return;
    if (append) { if (loadingMoreRef.current || !pageRef.current.hasMore || !pageRef.current.nextCursor) return; loadingMoreRef.current = true; setLoadingMore(true); } else setState("loading");
    try { const params = new URLSearchParams({ limit: "50" }); if (cursor) params.set("cursor", cursor); if (status !== "all") params.set("status", status); if (query) params.set("search", query); const [result, inventoryResult] = await Promise.all([client.request<import("../lib/api").OrderPage>(`/api/v1/organizations/${organization.id}/orders?${params}`), append ? Promise.resolve(null) : client.request<import("../lib/api").InventoryPage>(`/api/v1/organizations/${organization.id}/inventory?limit=100`)]); if (requestGeneration !== generation.current) return; setOrders((current) => append ? appendUnique(current, result.orders) : result.orders); if (inventoryResult) { setInventory(inventoryResult.inventory); setSelectedInventoryID((current) => current || inventoryResult.inventory.find((item) => item.available_quantity > 0)?.id || inventoryResult.inventory[0]?.id || ""); } pageRef.current = { nextCursor: result.next_cursor ?? "", hasMore: result.has_more }; setNextCursor(result.next_cursor ?? ""); setHasMore(result.has_more); setError(null); setState("ready"); } catch (loadError) { if (requestGeneration !== generation.current) return; setError(loadError); if (append) setState("ready"); else setState("error"); } finally { if (requestGeneration === generation.current) { loadingMoreRef.current = false; setLoadingMore(false); } }
  }, [client, organization, query, status]);
  useEffect(() => {
    const requestGeneration = ++generation.current;
    pageRef.current = { nextCursor: "", hasMore: false };
    loadingMoreRef.current = false;
    const timer = window.setTimeout(() => {
      setOrders([]);
      setNextCursor("");
      setHasMore(false);
      setError(null);
      void load("", false, requestGeneration);
    }, 0);
    return () => window.clearTimeout(timer);
  }, [load]);
  if (state === "loading") return <><PageHeading eyebrow="Order flow" title="Orders" description="Review order status, value, and warehouse allocation at a glance." /><LoadingState label="Loading orders" /></>;
  if (state === "error") return <><PageHeading eyebrow="Order flow" title="Orders" description="Review order status, value, and warehouse allocation at a glance." />{apiStatus(error) === 403 ? <PermissionDeniedState /> : <ErrorState onRetry={() => void load()} />}</>;
  const visibleOrders = orders.filter((order) => `${order.id} ${order.status} ${order.allocation_method}`.toLowerCase().includes(query.toLowerCase()) && (status === "all" || order.status === status));
  async function createOrder(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!organization || !selectedInventoryID) return;
    setCreating(true); setCreateError(""); setCreatedOrderID("");
    try {
      const created = await client.request<Order>(`/api/v1/organizations/${organization.id}/orders`, { method: "POST", headers: { "Idempotency-Key": `web-order-${crypto.randomUUID()}` }, body: JSON.stringify({ items: [{ inventory_level_id: selectedInventoryID, quantity: Number(quantity) }] }) });
      setCreatedOrderID(created.id); setOrders((current) => [created, ...current.filter((item) => item.id !== created.id)]); setQuantity("1");
      setInventory((current) => current.map((item) => item.id === selectedInventoryID ? { ...item, available_quantity: item.available_quantity - Number(quantity), reserved_quantity: item.reserved_quantity + Number(quantity) } : item));
    } catch (creationError) { setCreateError(creationError instanceof Error ? creationError.message : "The order could not be created."); } finally { setCreating(false); }
  }
  return <><PageHeading eyebrow="Order flow" title="Orders" description="Review order status, value, and warehouse allocation at a glance." />{canOperate(organization?.role) && <section className="panel" aria-labelledby="create-order-heading"><div className="panel-heading"><div><p className="eyebrow">New work</p><h2 id="create-order-heading">Create order</h2></div></div><form className="toolbar" onSubmit={createOrder}><label><span>Inventory</span><select aria-label="Inventory" value={selectedInventoryID} onChange={(event) => setSelectedInventoryID(event.target.value)} required>{inventory.map((item) => <option key={item.id} value={item.id}>{item.product_name} · {item.warehouse_name} · {item.available_quantity} available</option>)}</select></label><label><span>Quantity</span><input aria-label="Quantity" type="number" min="1" max="1000000" value={quantity} onChange={(event) => setQuantity(event.target.value)} required /></label><button className="primary-button" disabled={creating || inventory.length === 0}>{creating ? "Creating..." : "Create order"}</button></form>{createdOrderID && <p className="form-message" role="status">Order <Link className="table-link" href={`/orders/${createdOrderID}`}>#{createdOrderID.slice(0, 8)}</Link> created and stock reserved.</p>}{createError && <p className="form-message error" role="alert">{createError}</p>}</section>}<section className="panel"><div className="toolbar"><label className="search-field"><span className="sr-only">Search orders</span><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search by order ID, status, or allocation" /></label><label><span className="sr-only">Filter by status</span><select value={status} onChange={(event) => setStatus(event.target.value)}><option value="all">All statuses</option>{Array.from(new Set(orders.map((order) => order.status))).map((item) => <option key={item} value={item}>{item.replaceAll("_", " ")}</option>)}</select></label></div>{visibleOrders.length === 0 ? <EmptyState title={orders.length === 0 ? "No orders yet" : "No matching orders"} message={orders.length === 0 ? "Orders will appear here when work is created." : "Try a different search or status filter."} /> : <div className="table-wrap"><table><thead><tr><th>Order</th><th>Status</th><th>Subtotal</th><th>Allocation</th><th>Created</th></tr></thead><tbody>{visibleOrders.map((order) => <tr key={order.id}><td><Link className="table-link" href={`/orders/${order.id}`}>#{order.id.slice(0, 8)}</Link><span className="table-subtext">{order.items.length} item{order.items.length === 1 ? "" : "s"}</span></td><td><StatusBadge status={order.status} /></td><td>{formatMoney(order.subtotal_minor, order.currency_code ?? "AUD")}</td><td><span className="table-subtext">{order.allocation_method}{order.allocation_strategy ? ` · ${order.allocation_strategy}` : ""}</span></td><td>{dateTime.format(new Date(order.created_at))}</td></tr>)}</tbody></table></div>}{hasMore && <button className="secondary-button pagination-button" onClick={() => void load(nextCursor, true, generation.current)} disabled={loadingMore}>{loadingMore ? "Loading..." : "Load more"}</button>}</section></>;
}

function OrderDetailBase({ orderID }: { orderID: string }) {
  const { client, organization } = useSession();
  const [order, setOrder] = useState<Order | null>(null);
  const [payments, setPayments] = useState<Payment[]>([]);
  const [fulfillments, setFulfillments] = useState<Fulfillment[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<unknown>(null);
  const load = useCallback(async () => {
    if (!organization) return;
    setState("loading");
    try { const [orderResult, paymentResult, fulfillmentResult] = await Promise.all([client.request<Order>(`/api/v1/organizations/${organization.id}/orders/${orderID}`), client.request<{ payments: Payment[] }>(`/api/v1/organizations/${organization.id}/orders/${orderID}/payments`), client.request<{ fulfillments: Fulfillment[] }>(`/api/v1/organizations/${organization.id}/orders/${orderID}/fulfillments`)]); setOrder(orderResult); setPayments(paymentResult.payments); setFulfillments(fulfillmentResult.fulfillments); setState("ready"); } catch (loadError) { setError(loadError); setState("error"); }
  }, [client, orderID, organization]);
  useEffect(() => { const timer = window.setTimeout(() => { void load(); }, 0); return () => window.clearTimeout(timer); }, [load]);
  if (state === "loading") return <LoadingState label="Loading order" />;
  if (state === "error") return apiStatus(error) === 403 ? <PermissionDeniedState /> : <ErrorState message={apiStatus(error) === 404 ? "This order could not be found." : undefined} onRetry={() => void load()} />;
  if (!order) return <EmptyState title="Order unavailable" message="This order is no longer available in the selected organization." />;
  return <><div className="detail-back"><Link href="/orders" className="text-button">Back to orders</Link></div><div className="detail-heading"><div><p className="eyebrow">Order detail</p><h1>#{order.id.slice(0, 8)}</h1><p className="page-description">Created {dateTime.format(new Date(order.created_at))}</p></div><StatusBadge status={order.status} /></div><div className="detail-grid"><section className="panel panel-wide"><div className="panel-heading"><div><p className="eyebrow">Line items</p><h2>Order contents</h2></div><strong className="detail-total">{formatMoney(order.subtotal_minor, order.currency_code ?? "AUD")}</strong></div><div className="item-list">{order.items.map((item) => <div className="item-row" key={item.id}><div><strong>{item.product_name_snapshot}</strong><span>{item.sku_snapshot} · Qty {item.quantity}</span></div><strong>{formatMoney(item.line_total_minor, item.currency_code_snapshot ?? order.currency_code ?? "AUD")}</strong></div>)}</div></section><section className="panel"><p className="eyebrow">Allocation</p><h2>Warehouse plan</h2><dl className="detail-list"><div><dt>Method</dt><dd>{order.allocation_method}</dd></div><div><dt>Strategy</dt><dd>{order.allocation_strategy ?? "Not specified"}</dd></div><div><dt>Warehouses</dt><dd>{order.warehouses_used?.join(", ") || "Not allocated"}</dd></div></dl></section><section className="panel"><p className="eyebrow">Payments</p><h2>Payment attempts</h2>{payments.length === 0 ? <EmptyState title="No payment attempts" message="Payment details are not available for this order." /> : <div className="compact-list">{payments.map((payment) => <div className="list-row" key={payment.id}><div><strong>{formatMoney(payment.amount_minor, payment.currency_code)}</strong><span>{payment.provider_name ?? "Internal"} · {dateTime.format(new Date(payment.created_at))}</span></div><StatusBadge status={payment.status} /></div>)}</div>}</section><section className="panel"><p className="eyebrow">Fulfilment</p><h2>Fulfilment records</h2>{fulfillments.length === 0 ? <EmptyState title="Not fulfilled yet" message="Fulfilment records will appear when the order is completed." /> : <div className="compact-list">{fulfillments.map((fulfillment) => <div className="list-row" key={fulfillment.id}><div><strong>{fulfillment.warehouse_name}</strong><span>{fulfillment.warehouse_code} · {dateTime.format(new Date(fulfillment.created_at))}</span></div><StatusBadge status={fulfillment.status} /></div>)}</div>}</section></div></>;
}

export function OrderDetailView({ orderID }: { orderID: string }) {
  const { client, organization } = useSession();
  const [timeline, setTimeline] = useState<AuditEvent[]>([]);
  const [error, setError] = useState<unknown>(null);
  const load = useCallback(async () => {
    if (!organization) return;
    try {
      const result = await client.request<{ events: AuditEvent[] }>(`/api/v1/organizations/${organization.id}/orders/${orderID}/timeline`);
      setTimeline(result.events);
    } catch (loadError) {
      setError(loadError);
    }
  }, [client, orderID, organization]);
  useEffect(() => { const timer = window.setTimeout(() => void load(), 0); return () => window.clearTimeout(timer); }, [load]);
  return <><OrderDetailBase orderID={orderID} />{error ? <ErrorState message="The order timeline could not be loaded." onRetry={() => void load()} /> : <section className="panel order-timeline-panel"><p className="eyebrow">Operational history</p><h2>Order timeline</h2><OrderTimeline events={timeline} /></section>}</>;
}

function ReplenishmentPagedView({ lowStock, lowStockPage, lowStockLoading, lowStockError, onLoadMore, organization, warehouses }: { lowStock: LowStockItem[]; lowStockPage: { nextCursor: string; hasMore: boolean }; lowStockLoading: boolean; lowStockError: boolean; onLoadMore: () => void; organization: ReturnType<typeof useSession>["organization"]; warehouses: [string, string][] }) {
  return <section className="panel replenishment-panel"><div className="panel-heading"><div><p className="eyebrow">M16 recommendations</p><h2>Replenishment queue</h2></div>{lowStockPage.hasMore && <button className="secondary-button" onClick={onLoadMore} disabled={lowStockLoading || !lowStockPage.nextCursor}>{lowStockLoading ? "Loading..." : "Load more"}</button>}</div>{lowStockError && <p className="form-message error" role="alert">The next replenishment page could not be loaded.</p>}{lowStock.length === 0 ? <EmptyState title="All inventory is above its reorder point" message="No replenishment recommendations are waiting for review." /> : <div className="recommendation-grid">{lowStock.map((item) => <article className="recommendation-card" key={item.id}><div className="recommendation-top"><div><strong>{item.product_name}</strong><span>{item.warehouse_name}</span></div><StatusBadge status={item.status} /></div><div className="recommendation-metrics"><div><span>Available</span><strong>{item.available_quantity}</strong></div><div><span>Need</span><strong>{item.recommended_quantity}</strong></div><div><span>Unfulfilled</span><strong>{item.unfulfilled_quantity}</strong></div></div><div className="donor-list"><span className="muted-label">Recommended donors</span>{item.donors_truncated && <span className="table-subtext">Showing the strongest donor matches among the bounded recommendation set.</span>}{item.donor_allocations?.length ? item.donor_allocations.map((donor) => <span key={donor.source_inventory_level_id}>{warehouses.find(([id]) => id === donor.source_warehouse_id)?.[1] ?? donor.source_warehouse_id.slice(0, 8)} · {donor.quantity} units {canOperate(organization?.role) && <Link className="text-button" href={replenishmentTransferHref(item, donor)}>Create transfer</Link>}</span>) : <span>No donor allocation available</span>}</div></article>)}</div>}</section>;
}

export function InventoryView() {
  const { client, organization } = useSession();
  const [inventory, setInventory] = useState<InventoryLevel[]>([]);
  const [lowStock, setLowStock] = useState<LowStockItem[]>([]);
  const [query, setQuery] = useState("");
  const [warehouse, setWarehouse] = useState("all");
  const [status, setStatus] = useState("all");
  const [nextCursor, setNextCursor] = useState("");
  const [loadingMore, setLoadingMore] = useState(false);
  const [continuationError, setContinuationError] = useState(false);
  const [lowStockLoading, setLowStockLoading] = useState(false);
  const [lowStockError, setLowStockError] = useState(false);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<unknown>(null);
  const generation = useRef(0);
  const loadingMoreRef = useRef(false);
  const inventoryPageRef = useRef({ nextCursor: "", hasMore: false });
  const [lowStockPage, setLowStockPage] = useState({ nextCursor: "", hasMore: false });
  const load = useCallback(async (cursor = "", append = false, requestGeneration = generation.current) => {
    if (!organization || requestGeneration !== generation.current) return;
    if (append) {
      if (loadingMoreRef.current || !inventoryPageRef.current.hasMore || !inventoryPageRef.current.nextCursor) return;
      loadingMoreRef.current = true;
      setLoadingMore(true);
    } else {
      setInventory([]);
      setLowStock([]);
      setNextCursor("");
      setError(null);
      setLowStockError(false);
      setContinuationError(false);
      setState("loading");
    }
    try {
      setContinuationError(false);
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      if (warehouse !== "all") params.set("warehouse_id", warehouse);
      const inventoryRequest = client.request<import("../lib/api").InventoryPage>(`/api/v1/organizations/${organization.id}/inventory?${params}`);
      const lowStockRequest: Promise<import("../lib/api").LowStockPage | null> = append ? Promise.resolve(null) : client.request<import("../lib/api").LowStockPage>(`/api/v1/organizations/${organization.id}/inventory/low-stock?limit=50`);
      const [inventoryResult, lowStockResult] = await Promise.allSettled([inventoryRequest, lowStockRequest]);
      if (requestGeneration !== generation.current) return;

      if (inventoryResult.status === "fulfilled") {
        setInventory((current) => append ? appendUnique(current, inventoryResult.value.inventory) : inventoryResult.value.inventory);
        const nextPage = { nextCursor: inventoryResult.value.next_cursor ?? "", hasMore: inventoryResult.value.has_more };
        inventoryPageRef.current = nextPage;
        setNextCursor(nextPage.nextCursor);
        setError(null);
        setContinuationError(false);
      } else if (inventoryResult.status === "rejected") {
        setError(inventoryResult.reason);
        setContinuationError(true);
      }

      if (lowStockResult.status === "fulfilled" && lowStockResult.value) {
        setLowStock(lowStockResult.value.inventory);
        setLowStockPage({ nextCursor: lowStockResult.value.next_cursor ?? "", hasMore: lowStockResult.value.has_more });
        setLowStockError(false);
      } else if (lowStockResult.status === "rejected") {
        setLowStockError(true);
      }

      const inventoryFailed = inventoryResult.status === "rejected";
      const lowStockFailed = lowStockResult.status === "rejected";
      if (!append && inventoryFailed && lowStockFailed) {
        setState("error");
      } else {
        setState("ready");
      }
    } catch (loadError) {
      if (requestGeneration !== generation.current) return;
      setError(loadError);
      if (append) {
        setContinuationError(true);
        setState("ready");
      } else {
        setState("error");
      }
    } finally {
      if (requestGeneration === generation.current) {
        loadingMoreRef.current = false;
        setLoadingMore(false);
      }
    }
  }, [client, organization, warehouse]);
  const loadLowStock = useCallback(async (cursor = "", requestGeneration = generation.current) => {
    if (!organization || requestGeneration !== generation.current || lowStockLoading) return;
    setLowStockLoading(true);
    try {
      const query = new URLSearchParams({ limit: "50" });
      if (cursor) query.set("cursor", cursor);
      const result = await client.request<import("../lib/api").LowStockPage>(`/api/v1/organizations/${organization.id}/inventory/low-stock?${query}`);
      if (requestGeneration !== generation.current) return;
      setLowStock((current) => cursor ? appendUnique(current, result.inventory) : result.inventory);
      setLowStockPage({ nextCursor: result.next_cursor ?? "", hasMore: result.has_more });
      setLowStockError(false);
    } catch {
      if (requestGeneration === generation.current) setLowStockError(true);
    } finally {
      if (requestGeneration === generation.current) setLowStockLoading(false);
    }
  }, [client, lowStockLoading, organization]);
  useEffect(() => { const requestGeneration = ++generation.current; loadingMoreRef.current = false; const timer = window.setTimeout(() => { void load("", false, requestGeneration); }, 0); return () => window.clearTimeout(timer); }, [client, organization, warehouse, load]);
  if (state === "loading") return <><PageHeading eyebrow="Stock control" title="Inventory" description="Understand availability and act on replenishment signals before they become stockouts." /><LoadingState label="Loading inventory" /></>;
  if (state === "error") return <><PageHeading eyebrow="Stock control" title="Inventory" description="Understand availability and act on replenishment signals before they become stockouts." />{apiStatus(error) === 403 ? <PermissionDeniedState /> : <ErrorState onRetry={() => void load()} />}</>;
  const rows = inventory.map((item) => ({ ...item, status: inventoryStatus(item) })).filter((item) => `${item.product_name} ${item.product_sku} ${item.warehouse_name}`.toLowerCase().includes(query.toLowerCase()) && (warehouse === "all" || item.warehouse_id === warehouse) && (status === "all" || item.status === status));
  const warehouses = Array.from(new Map(inventory.map((item) => [item.warehouse_id, item.warehouse_name])).entries());
  return <InventorySections inventory={<><InventoryPagedView rows={rows} inventoryCount={inventory.length} query={query} warehouse={warehouse} status={status} warehouses={warehouses} onQuery={setQuery} onWarehouse={setWarehouse} onStatus={setStatus} onLoadMore={() => void load(nextCursor, true, generation.current)} loadingMore={loadingMore} continuationError={continuationError} /><button className="secondary-button pagination-button" onClick={() => void load(nextCursor, true, generation.current)} disabled={loadingMore || !nextCursor}>{loadingMore ? "Loading..." : "Load more"}</button></>} replenishment={<ReplenishmentPagedView lowStock={lowStock} lowStockPage={lowStockPage} lowStockLoading={lowStockLoading} lowStockError={lowStockError} onLoadMore={() => void loadLowStock(lowStockPage.nextCursor, generation.current)} organization={organization} warehouses={warehouses} />} />;
}

function InventoryPagedView({ rows, inventoryCount, query, warehouse, status, warehouses, onQuery, onWarehouse, onStatus, onLoadMore, loadingMore, continuationError }: { rows: (InventoryLevel & { status: LowStockItem["status"] })[]; inventoryCount: number; query: string; warehouse: string; status: string; warehouses: [string, string][]; onQuery: (value: string) => void; onWarehouse: (value: string) => void; onStatus: (value: string) => void; onLoadMore: () => void; loadingMore: boolean; continuationError: boolean }) {
  return <><PageHeading eyebrow="Stock control" title="Inventory" description="Understand availability and act on replenishment signals before they become stockouts." />{continuationError && <ErrorState message="The next inventory page could not be loaded." onRetry={onLoadMore} />}<section className="panel"><div className="toolbar toolbar-wrap"><label className="search-field"><span className="sr-only">Search inventory</span><input value={query} onChange={(event) => onQuery(event.target.value)} placeholder="Search product or warehouse" /></label><label><span className="sr-only">Filter by warehouse</span><select value={warehouse} onChange={(event) => onWarehouse(event.target.value)}><option value="all">All warehouses</option>{warehouses.map(([id, name]) => <option value={id} key={id}>{name}</option>)}</select></label><label><span className="sr-only">Filter by stock status</span><select value={status} onChange={(event) => onStatus(event.target.value)}><option value="all">All stock statuses</option><option value="healthy">Healthy</option><option value="low">Low</option><option value="out_of_stock">Out of stock</option><option value="unconfigured">Unconfigured</option></select></label></div>{rows.length === 0 ? <EmptyState title={inventoryCount === 0 ? "No inventory configured" : "No matching inventory"} message="Try a different search or filter." /> : <div className="table-wrap"><table><thead><tr><th>Product</th><th>Warehouse</th><th>On hand</th><th>Available</th><th>Reserved</th><th>Policy</th><th>Status</th></tr></thead><tbody>{rows.map((item) => <tr key={item.id}><td><strong>{item.product_name}</strong><span className="table-subtext">{item.product_sku}</span></td><td>{item.warehouse_name}</td><td>{item.on_hand_quantity}</td><td>{item.available_quantity}</td><td>{item.reserved_quantity}</td><td><span className="table-subtext">Reorder {item.reorder_point ?? "—"} · Target {item.target_stock_level ?? "—"}</span></td><td><StatusBadge status={item.status} /></td></tr>)}</tbody></table></div>}<button className="secondary-button pagination-button" onClick={onLoadMore} disabled={loadingMore}>{loadingMore ? "Loading..." : "Load more"}</button></section></>;
}