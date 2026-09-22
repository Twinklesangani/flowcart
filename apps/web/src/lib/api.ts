export type Role = "owner" | "admin" | "warehouse_manager" | "support" | "viewer";

export type User = {
  id: string;
  email: string;
  first_name: string;
  last_name: string;
};

export type Organization = { id: string; name: string; slug: string; role: Role };
export type PageMeta = { next_cursor?: string; has_more: boolean };
export type ProductPage = PageMeta & { products: Product[] };
export type WarehousePage = PageMeta & { warehouses: Warehouse[] };
export type InventoryPage = PageMeta & { inventory: InventoryLevel[] };
export type LowStockPage = PageMeta & { inventory: LowStockItem[] };
export type OrderPage = PageMeta & { orders: Order[] };
export type TransferPage = PageMeta & { transfers: Transfer[] };
export type DashboardWarehouseMetric = { id: string; code: string; name: string; inventory_count: number; available_quantity: number };
export type DashboardMetrics = { order_count: number; open_order_count: number; paid_order_count: number; inventory_count: number; warehouse_count: number; low_stock_count: number; out_of_stock_count: number; transfer_count: number; warehouses: DashboardWarehouseMetric[] };

export type OrderItem = {
  id: string;
  product_id: string;
  product_name_snapshot: string;
  sku_snapshot: string;
  quantity: number;
  unit_price_minor_snapshot?: number | null;
  currency_code_snapshot?: string | null;
  line_total_minor?: number | null;
  reservation_id?: string | null;
  reservation_status?: string | null;
  allocations?: Allocation[];
  created_at: string;
};

export type Product = { id: string; sku: string; name: string };
export type Warehouse = { id: string; code: string; name: string };

export type Allocation = {
  warehouse: string;
  inventory_level_id: string;
  reserved_quantity: number;
};

export type Order = {
  id: string;
  status: string;
  allocation_method: string;
  allocation_strategy?: string | null;
  created_at: string;
  updated_at: string;
  cancelled_at?: string | null;
  paid_at?: string | null;
  currency_code?: string | null;
  subtotal_minor?: number | null;
  items: OrderItem[];
  allocations?: Allocation[];
  warehouses_used?: string[];
};

export type InventoryLevel = {
  id: string;
  product_id: string;
  product_sku: string;
  product_name: string;
  warehouse_id: string;
  warehouse_code: string;
  warehouse_name: string;
  on_hand_quantity: number;
  reserved_quantity: number;
  available_quantity: number;
  reorder_point?: number | null;
  target_stock_level?: number | null;
  created_at: string;
  updated_at: string;
};

export type DonorAllocation = {
  source_warehouse_id: string;
  source_inventory_level_id: string;
  quantity: number;
};

export type LowStockItem = InventoryLevel & {
  status: "unconfigured" | "out_of_stock" | "low" | "healthy";
  recommended_quantity: number;
  unfulfilled_quantity: number;
  donors_truncated?: boolean;
  donor_allocations?: DonorAllocation[];
};

export type Payment = {
  id: string;
  status: string;
  amount_minor: number;
  currency_code: string;
  provider_name?: string | null;
  provider_status?: string | null;
  created_at: string;
};

export type Fulfillment = {
  id: string;
  warehouse_code: string;
  warehouse_name: string;
  status: string;
  created_at: string;
  completed_at: string;
  items: { order_item_id: string; quantity: number }[];
};

export type TransferItem = {
  id: string;
  product_id: string;
  product_sku: string;
  product_name: string;
  source_inventory_level_id: string;
  destination_inventory_level_id: string;
  quantity: number;
  created_at: string;
};

export type Transfer = {
  id: string;
  source_warehouse_id: string;
  destination_warehouse_id: string;
  source_warehouse_code: string;
  destination_warehouse_code: string;
  status: "pending" | "in_transit" | "completed" | "cancelled";
  created_by_user_id: string;
  dispatched_by_user_id?: string | null;
  received_by_user_id?: string | null;
  cancelled_by_user_id?: string | null;
  created_at: string;
  dispatched_at?: string | null;
  completed_at?: string | null;
  cancelled_at?: string | null;
  items: TransferItem[];
};

export type AuditEvent = {
  id: string;
  organization_id: string;
  event_type: string;
  resource_type: string;
  resource_id: string;
  parent_resource_type?: string | null;
  parent_resource_id?: string | null;
  actor_type: "user" | "system" | "provider";
  actor_user_id?: string | null;
  source_type: string;
  source_id: string;
  metadata: Record<string, unknown>;
  created_at: string;
};

export type ApiError = { status: number; code: string; message: string };

const apiUrl = process.env.NEXT_PUBLIC_API_MODE === "proxy" ? "" : process.env.NEXT_PUBLIC_API_URL ?? "";

function errorMessage(status: number, code: string): string {
  if (status === 401) return "Your session has expired. Please sign in again.";
  if (status === 403) return "You do not have permission to perform this action.";
  if (status === 404) return "The requested record could not be found.";
  if (status === 409) return "This action conflicts with the current operational state.";
  if (code === "internal_error") return "The service is temporarily unavailable. Try again.";
  return "The request could not be completed. Try again.";
}

export class ApiClient {
  private pendingRefreshes = new Set<Promise<{ access_token: string }>>();
  private refreshSuppressed: () => boolean = () => false;
  private refreshPromise: Promise<{ access_token: string }> | null = null;
  private refreshGeneration = 0;
  private sessionGeneration = 0;
  private accessToken: string | null = null;
  private onToken: ((token: string) => void) | null = null;
  private onExpired: (() => void) | null = null;

  setSession(token: string | null, onToken?: (token: string) => void, onExpired?: () => void) {
    this.accessToken = token;
    this.onToken = onToken ?? null;
    this.onExpired = onExpired ?? null;
  }

  invalidateSession() {
    this.sessionGeneration += 1;
    this.refreshPromise = null;
    this.accessToken = null;
    this.onToken = null;
    this.onExpired = null;
  }

  setRefreshGuard(guard: () => boolean) {
    this.refreshSuppressed = guard;
  }

  async waitForRefreshes() {
    await Promise.allSettled([...this.pendingRefreshes]);
  }

  refreshSession(): Promise<{ access_token: string }> {
    if (this.refreshSuppressed()) return Promise.reject(new Error("Please sign in again."));
    const generation = this.sessionGeneration;
    if (!this.refreshPromise || this.refreshGeneration !== generation) {
      const promise = this.request<{ access_token: string }>("/api/v1/auth/refresh", { method: "POST", signal: AbortSignal.timeout(15000) }, false);
      this.pendingRefreshes.add(promise);
      this.refreshPromise = promise;
      this.refreshGeneration = generation;
      const clearRefresh = () => {
        this.pendingRefreshes.delete(promise);
        if (this.refreshPromise === promise) this.refreshPromise = null;
      };
      void promise.then(clearRefresh, clearRefresh);
    }
    return this.refreshPromise;
  }

  async request<T>(path: string, options: RequestInit = {}, retry = true): Promise<T> {
    const generation = this.sessionGeneration;
    const response = await fetch(`${apiUrl}${path}`, {
      ...options,
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        ...(this.accessToken ? { Authorization: `Bearer ${this.accessToken}` } : {}),
        ...options.headers,
      },
    });

    if (generation !== this.sessionGeneration) throw new Error("Your session has ended. Please sign in again.");

    if (response.status === 401 && retry && !path.endsWith("/auth/refresh")) {
      if (generation !== this.sessionGeneration) throw new Error("session invalidated");
      try {
        const refreshed = await this.refreshSession();
        if (generation !== this.sessionGeneration || this.refreshSuppressed()) throw new Error("session invalidated");
        this.accessToken = refreshed.access_token;
        this.onToken?.(refreshed.access_token);
        return this.request<T>(path, options, false);
      } catch {
        if (generation === this.sessionGeneration) {
          this.accessToken = null;
          this.onExpired?.();
        }
      }
    }

    if (!response.ok) {
      let body: { error?: { code?: string; message?: string } } = {};
      try {
        body = (await response.json()) as typeof body;
      } catch {
        // Use the status-specific fallback below.
      }
      const code = body.error?.code ?? "request_failed";
      const error = new Error(body.error?.message ?? errorMessage(response.status, code)) as Error & { apiError: ApiError };
      error.apiError = { status: response.status, code, message: error.message };
      throw error;
    }

    if (response.status === 204) return undefined as T;
    const body = (await response.json()) as T;
    if (generation !== this.sessionGeneration) throw new Error("Your session has ended. Please sign in again.");
    return body;
  }
}

export const apiClient = new ApiClient();
