"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { AuditEvent, Transfer } from "../lib/api";
import { canOperate, useSession } from "../lib/session";
import { EmptyState, ErrorState, LoadingState, PermissionDeniedState } from "./states";
import { StatusBadge } from "./operations";
import { appendUnique } from "../lib/pagination-state";
import { TransferCreateController } from "../lib/transfer-create-controller";
import { isCurrentGeneration } from "../lib/request-generation";

const dateTime = new Intl.DateTimeFormat("en-AU", { dateStyle: "medium", timeStyle: "short" });
const eventLabels: Record<string, string> = { "transfer.created": "Transfer created", "transfer.dispatched": "Transfer dispatched", "transfer.received": "Transfer received", "transfer.cancelled": "Transfer cancelled", "order.created": "Order created", "payment.created": "Payment created", "payment.succeeded": "Payment succeeded", "order.paid": "Order paid", "fulfillment.completed": "Fulfilment completed", "order.fulfilled": "Order fulfilled" };

function apiStatus(error: unknown) { return (error as { apiError?: { status?: number } })?.apiError?.status; }
function label(value: string) { return value.replaceAll("_", " "); }
function eventLabel(event: string) { return eventLabels[event] ?? label(event); }
function metadataSummary(metadata: Record<string, unknown>) { return Object.entries(metadata).filter(([key]) => !/token|secret|hash|payload|body/i.test(key)).slice(0, 3).map(([key, value]) => `${label(key)}: ${String(value)}`).join(" · "); }
function operationKey() { return typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`; }

export function Timeline({ events, emptyMessage = "No operational events recorded yet." }: { events: AuditEvent[]; emptyMessage?: string }) {
  if (!events.length) return <EmptyState title="No timeline events" message={emptyMessage} />;
  return <ol className="timeline">{events.map((event) => <li key={event.id} className="timeline-item"><span className="timeline-marker" aria-hidden="true" /><div><div className="timeline-top"><strong>{eventLabel(event.event_type)}</strong><time dateTime={event.created_at}>{dateTime.format(new Date(event.created_at))}</time></div><span className="timeline-actor">{event.actor_type}{event.actor_user_id ? ` · ${event.actor_user_id.slice(0, 8)}` : ""}</span>{metadataSummary(event.metadata) && <span className="timeline-meta">{metadataSummary(event.metadata)}</span>}</div></li>)}</ol>;
}

export function TransfersView() {
  const { client, organization } = useSession();
  const [transfers, setTransfers] = useState<Transfer[]>([]);
  const [status, setStatus] = useState("all");
  const [nextCursor, setNextCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<unknown>(null);
  const generation = useRef(0);
  const loadingMoreRef = useRef(false);
  const [pageState, setPageState] = useState({ nextCursor: "", hasMore: false });
  const load = useCallback(async (cursor = "", append = false, requestGeneration = generation.current) => {
    if (!organization || !isCurrentGeneration(requestGeneration, generation.current)) return;
    if (append) {
      if (loadingMoreRef.current || !pageState.nextCursor || !pageState.hasMore) return;
      loadingMoreRef.current = true;
      setLoadingMore(true);
    } else {
      setTransfers([]);
      setNextCursor("");
      setHasMore(false);
      setError(null);
      setState("loading");
    }
    try {
      const params = new URLSearchParams({ limit: "50" });
      if (cursor) params.set("cursor", cursor);
      if (status !== "all") params.set("status", status);
      const result = await client.request<import("../lib/api").TransferPage>(`/api/v1/organizations/${organization.id}/transfers?${params}`);
      if (!isCurrentGeneration(requestGeneration, generation.current)) return;
      setTransfers((current) => append ? appendUnique(current, result.transfers) : result.transfers);
      setPageState({ nextCursor: result.next_cursor ?? "", hasMore: result.has_more });
      setNextCursor(result.next_cursor ?? "");
      setHasMore(result.has_more);
      setError(null);
      setState("ready");
    } catch (loadError) {
      if (!isCurrentGeneration(requestGeneration, generation.current)) return;
      setError(loadError);
      if (append) setState("ready"); else setState("error");
    } finally {
      if (requestGeneration === generation.current) {
        loadingMoreRef.current = false;
        setLoadingMore(false);
      }
    }
  }, [client, organization, pageState, status]);
  useEffect(() => { const requestGeneration = ++generation.current; loadingMoreRef.current = false; const timer = window.setTimeout(() => { void load("", false, requestGeneration); }, 0); return () => window.clearTimeout(timer); }, [load]);
  if (state === "loading") return <><WorkflowHeading title="Transfers" description="Move stock between warehouses with an auditable operational handoff." /><LoadingState label="Loading transfers" /></>;
  if (state === "error") return <><WorkflowHeading title="Transfers" description="Move stock between warehouses with an auditable operational handoff." />{apiStatus(error) === 403 ? <PermissionDeniedState /> : <ErrorState onRetry={() => void load()} />}</>;
  const visible = transfers.filter((item) => status === "all" || item.status === status);
  return <><WorkflowHeading title="Transfers" description="Move stock between warehouses with an auditable operational handoff." /><section className="panel"><div className="toolbar"><label><span className="sr-only">Filter transfers by status</span><select value={status} onChange={(event) => setStatus(event.target.value)}><option value="all">All statuses</option><option value="pending">Pending</option><option value="in_transit">In transit</option><option value="completed">Completed</option><option value="cancelled">Cancelled</option></select></label></div>{visible.length === 0 ? <EmptyState title={transfers.length ? "No matching transfers" : "No warehouse transfers yet"} message={transfers.length ? "Try another status filter." : "Transfers will appear here when stock is moved between warehouses."} /> : <div className="table-wrap"><table><thead><tr><th>Transfer</th><th>Route</th><th>Status</th><th>Items</th><th>Created</th><th>Dispatched</th><th>Completed</th></tr></thead><tbody>{visible.map((transfer) => <tr key={transfer.id}><td><Link className="table-link" href={`/transfers/${transfer.id}`}>#{transfer.id.slice(0, 8)}</Link></td><td>{transfer.source_warehouse_code} → {transfer.destination_warehouse_code}</td><td><StatusBadge status={transfer.status} /></td><td>{transfer.items.length}</td><td>{dateTime.format(new Date(transfer.created_at))}</td><td>{transfer.dispatched_at ? dateTime.format(new Date(transfer.dispatched_at)) : "—"}</td><td>{transfer.completed_at ? dateTime.format(new Date(transfer.completed_at)) : "—"}</td></tr>)}</tbody></table></div>}{hasMore && <button className="secondary-button pagination-button" onClick={() => void load(nextCursor, true, generation.current)} disabled={loadingMore}>{loadingMore ? "Loading..." : "Load more"}</button>}</section></>;
}

function WorkflowHeading({ title, description }: { title: string; description: string }) { return <div className="page-heading"><div><p className="eyebrow">Workflow completion</p><h1>{title}</h1><p className="page-description">{description}</p></div></div>; }

export function TransferDetailView({ transferID }: { transferID: string }) {
  const { client, organization } = useSession();
  const [transfer, setTransfer] = useState<Transfer | null>(null);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<unknown>(null);
  const [confirmAction, setConfirmAction] = useState<"dispatch" | "receive" | "cancel" | null>(null);
  const [busy, setBusy] = useState(false);
  const load = useCallback(async () => { if (!organization) return; setState("loading"); try { const [transferResult, timelineResult] = await Promise.all([client.request<Transfer>(`/api/v1/organizations/${organization.id}/transfers/${transferID}`), client.request<{ events: AuditEvent[] }>(`/api/v1/organizations/${organization.id}/transfers/${transferID}/timeline`)]); setTransfer(transferResult); setEvents(timelineResult.events); setState("ready"); } catch (loadError) { setError(loadError); setState("error"); } }, [client, organization, transferID]);
  useEffect(() => { const timer = window.setTimeout(() => void load(), 0); return () => window.clearTimeout(timer); }, [load]);
  async function performAction() { if (!organization || !transfer || !confirmAction) return; setBusy(true); try { const path = `/api/v1/organizations/${organization.id}/transfers/${transfer.id}/${confirmAction === "cancel" ? "cancel" : confirmAction}`; await client.request<Transfer>(path, { method: "POST", ...(confirmAction === "cancel" ? {} : { headers: { "Idempotency-Key": operationKey() } }) }); setConfirmAction(null); await load(); } catch (actionError) { setError(actionError); setConfirmAction(null); } finally { setBusy(false); } }
  if (state === "loading") return <LoadingState label="Loading transfer" />;
  if (state === "error") return apiStatus(error) === 403 ? <PermissionDeniedState /> : <ErrorState message={apiStatus(error) === 404 ? "This transfer could not be found." : undefined} onRetry={() => void load()} />;
  if (!transfer) return <EmptyState title="Transfer unavailable" message="This transfer is not available in the selected organization." />;
  const writable = canOperate(organization?.role);
  const action = transfer.status === "pending" ? "dispatch" : transfer.status === "in_transit" ? "receive" : null;
  return <><div className="detail-back"><Link href="/transfers" className="text-button">Back to transfers</Link></div><div className="detail-heading"><div><p className="eyebrow">Transfer detail</p><h1>#{transfer.id.slice(0, 8)}</h1><p className="page-description">{transfer.source_warehouse_code} → {transfer.destination_warehouse_code}</p></div><div className="detail-actions"><StatusBadge status={transfer.status} />{writable && action && <><button className="primary-button" onClick={() => setConfirmAction(action)}>{label(action)}</button>{transfer.status === "pending" && <button className="secondary-button" onClick={() => setConfirmAction("cancel")}>Cancel</button>}</>}</div></div><div className="detail-grid"><section className="panel panel-wide"><p className="eyebrow">Items</p><h2>Transfer contents</h2><div className="item-list">{transfer.items.map((item) => <div className="item-row" key={item.id}><div><strong>{item.product_name}</strong><span>{item.product_sku}</span></div><strong>{item.quantity} units</strong></div>)}</div></section><section className="panel"><p className="eyebrow">Movement</p><h2>Actors &amp; timestamps</h2><dl className="detail-list"><div><dt>Created</dt><dd>{dateTime.format(new Date(transfer.created_at))}</dd></div><div><dt>Dispatched by</dt><dd>{transfer.dispatched_by_user_id?.slice(0, 8) ?? "—"}</dd></div><div><dt>Received by</dt><dd>{transfer.received_by_user_id?.slice(0, 8) ?? "—"}</dd></div></dl></section><section className="panel panel-wide"><p className="eyebrow">Operational history</p><h2>Transfer timeline</h2><Timeline events={events} emptyMessage="The transfer has no recorded audit events." /></section></div>{confirmAction && <ConfirmDialog action={confirmAction} busy={busy} onCancel={() => setConfirmAction(null)} onConfirm={() => void performAction()} />}</>;
}

function ConfirmDialog({ action, busy, onCancel, onConfirm }: { action: string; busy: boolean; onCancel: () => void; onConfirm: () => void }) { return <div className="dialog-backdrop" role="presentation"><section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="confirm-heading"><p className="eyebrow">Confirm action</p><h2 id="confirm-heading">{label(action)} this transfer?</h2><p>This will update the transfer state and inventory workflow. Continue only if the warehouse handoff is ready.</p><div className="dialog-actions"><button className="secondary-button" onClick={onCancel} disabled={busy}>Keep transfer</button><button className="primary-button" onClick={onConfirm} disabled={busy}>{busy ? "Working..." : label(action)}</button></div></section></div>; }

export function TransferCreateView({ source, destination, product, quantity }: { source?: string; destination?: string; product?: string; quantity?: string }) {
  const { client, organization } = useSession();
  const router = useRouter();
  const [amount, setAmount] = useState(quantity ?? "");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const [controller] = useState(() => new TransferCreateController((path, options) => client.request(path, options)));
  const [, refresh] = useState(0);
  useEffect(() => controller.subscribe(() => refresh((value) => value + 1)), [controller]);
  useEffect(() => { if (!organization) return; controller.resetOrganization(organization.id, { source, destination, product }); void controller.loadWarehouses(); void controller.loadProducts(); }, [controller, destination, organization, product, source]);
  async function submit(event: React.FormEvent<HTMLFormElement>) { event.preventDefault(); setMessage(""); if (!organization) return; setBusy(true); try { const transfer = await controller.submit(amount); router.push(`/transfers/${transfer.id}`); } catch (submitError) { setMessage(submitError instanceof Error ? submitError.message : "Unable to create transfer."); } finally { setBusy(false); } }
  const productOptions = controller.options("product");
  const warehouseOptions = controller.options("source");
  return <><div className="detail-back"><Link href="/inventory" className="text-button">Back to inventory</Link></div><WorkflowHeading title="Create transfer" description="Review the recommendation, then submit a standard warehouse transfer." /><section className="panel form-panel"><form className="transfer-form" onSubmit={submit}><label>Source warehouse<select value={controller.sourceID} onChange={(event) => void controller.select("source", event.target.value)} required><option value="">Select source</option>{warehouseOptions.map((item) => <option key={item.id} value={item.id}>{"code" in item ? `${item.code} · ${item.name}` : item.name}</option>)}</select></label><label>Destination warehouse<select value={controller.destinationID} onChange={(event) => void controller.select("destination", event.target.value)} required><option value="">Select destination</option>{warehouseOptions.map((item) => <option key={item.id} value={item.id}>{"code" in item ? `${item.code} · ${item.name}` : item.name}</option>)}</select></label><label>Product<select value={controller.productID} onChange={(event) => void controller.select("product", event.target.value)} required><option value="">Select product</option>{productOptions.map((item) => <option key={item.id} value={item.id}>{"sku" in item ? `${item.sku} · ${item.name}` : item.name}</option>)}</select></label><label>Quantity<input type="number" min="1" step="1" value={amount} onChange={(event) => setAmount(event.target.value)} required /></label>{message && <p className="form-message error" role="alert">{message}</p>}<button className="primary-button" disabled={busy}>{busy ? "Creating..." : "Create transfer"}</button></form></section></>;
}

export function AuditView() {
  const { client, organization } = useSession();
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [nextCursor, setNextCursor] = useState("");
  const [eventType, setEventType] = useState("");
  const [resourceType, setResourceType] = useState("");
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [error, setError] = useState<unknown>(null);
  const generation = useRef(0);
  const load = useCallback(async (cursor = "", requestGeneration = generation.current) => {
    if (!organization || !isCurrentGeneration(requestGeneration, generation.current)) return;
    if (!cursor) {
      setEvents([]);
      setNextCursor("");
      setError(null);
    }
    setState("loading");
    try {
      const query = new URLSearchParams({ limit: "50" });
      if (cursor) query.set("cursor", cursor);
      if (eventType) query.set("event_type", eventType);
      if (resourceType) query.set("resource_type", resourceType);
      const result = await client.request<{ events: AuditEvent[]; next_cursor?: string }>(`/api/v1/organizations/${organization.id}/audit-events?${query}`);
      if (!isCurrentGeneration(requestGeneration, generation.current)) return;
      setEvents((current) => cursor ? appendUnique(current, result.events) : result.events);
      setNextCursor(result.next_cursor ?? "");
      setError(null);
      setState("ready");
    } catch (loadError) {
      if (!isCurrentGeneration(requestGeneration, generation.current)) return;
      setError(loadError);
      setState("error");
    }
  }, [client, eventType, organization, resourceType]);
  useEffect(() => {
    const requestGeneration = ++generation.current;
    const timer = window.setTimeout(() => { void load("", requestGeneration); }, 0);
    return () => window.clearTimeout(timer);
  }, [load]);
  if (state === "loading" && !events.length) return <><WorkflowHeading title="Audit log" description="Review organization-wide operational events with stable, cursor-based history." /><LoadingState label="Loading audit events" /></>;
  if (state === "error") return <><WorkflowHeading title="Audit log" description="Review organization-wide operational events with stable, cursor-based history." />{apiStatus(error) === 403 ? <PermissionDeniedState /> : <ErrorState onRetry={() => void load()} />}</>;
  return <><WorkflowHeading title="Audit log" description="Review organization-wide operational events with stable, cursor-based history." /><section className="panel"><div className="toolbar toolbar-wrap"><label>Event type<input value={eventType} onChange={(event) => setEventType(event.target.value)} placeholder="e.g. transfer.created" /></label><label>Resource type<input value={resourceType} onChange={(event) => setResourceType(event.target.value)} placeholder="e.g. order" /></label><button className="secondary-button" onClick={() => void load()}>Apply filters</button></div>{events.length === 0 ? <EmptyState title="No audit events" message="Operational history will appear here as your organization works." /> : <div className="table-wrap"><table><thead><tr><th>Time</th><th>Event</th><th>Resource</th><th>Actor</th><th>Summary</th></tr></thead><tbody>{events.map((event) => <tr key={event.id}><td>{dateTime.format(new Date(event.created_at))}</td><td><strong>{eventLabel(event.event_type)}</strong></td><td>{event.resource_type} · {event.resource_id.slice(0, 8)}</td><td>{event.actor_type}{event.actor_user_id ? ` · ${event.actor_user_id.slice(0, 8)}` : ""}</td><td><span className="table-subtext">{metadataSummary(event.metadata) || "—"}</span></td></tr>)}</tbody></table></div>}{nextCursor && <button className="secondary-button pagination-button" onClick={() => void load(nextCursor)} disabled={state === "loading"}>{state === "loading" ? "Loading..." : "Load more"}</button>}</section></>;
}