import { Product, Transfer, Warehouse } from "./api";

export const transferSelectorSentinel = "__load_more__";
export const transferSelectorLoading = "__loading__";
export const transferSelectorError = "__error__";

type ProductPage = { products: Product[]; next_cursor?: string; has_more: boolean };
type WarehousePage = { warehouses: Warehouse[]; next_cursor?: string; has_more: boolean };
type Request = <T>(path: string, options?: RequestInit) => Promise<T>;
type SelectorKind = "product" | "source" | "destination";

export type TransferPayload = {
  source_warehouse_id: string;
  destination_warehouse_id: string;
  items: [{ product_id: string; quantity: number }];
};

export class TransferCreateController {
  private organizationID = "";
  private initialized = false;
  private request: Request;
  private listeners = new Set<() => void>();

  products: Product[] = [];
  warehouses: Warehouse[] = [];
  productCursor = "";
  warehouseCursor = "";
  productHasMore = false;
  warehouseHasMore = false;
  productLoading = false;
  warehouseLoading = false;
  productError = false;
  warehouseError = false;
  sourceID = "";
  destinationID = "";
  productID = "";

  constructor(request: Request) {
    this.request = request;
  }

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  private emit() {
    this.listeners.forEach((listener) => listener());
  }

  resetOrganization(organizationID: string, initial: { source?: string; destination?: string; product?: string } = {}) {
    const changed = this.organizationID !== organizationID;
    this.organizationID = organizationID;
    this.products = [];
    this.warehouses = [];
    this.productCursor = "";
    this.warehouseCursor = "";
    this.productHasMore = false;
    this.warehouseHasMore = false;
    this.productLoading = false;
    this.warehouseLoading = false;
    this.productError = false;
    this.warehouseError = false;
    if (!this.initialized) {
      this.sourceID = initial.source ?? "";
      this.destinationID = initial.destination ?? "";
      this.productID = initial.product ?? "";
    } else if (changed) {
      this.sourceID = "";
      this.destinationID = "";
      this.productID = "";
    }
    this.initialized = true;
    this.emit();
  }

  async loadProducts(append = false) {
    if (!this.organizationID || this.productLoading || (append && (!this.productHasMore || !this.productCursor))) return;
    this.productLoading = true;
    this.productError = false;
    this.emit();
    try {
      const query = new URLSearchParams({ limit: "50" });
      if (append) query.set("cursor", this.productCursor);
      const page = await this.request<ProductPage>(`/api/v1/organizations/${this.organizationID}/products?${query}`);
      this.products = this.uniqueAppend(append ? this.products : [], page.products);
      this.productCursor = page.next_cursor ?? "";
      this.productHasMore = page.has_more;
    } catch {
      this.productError = true;
    } finally {
      this.productLoading = false;
      this.emit();
    }
  }

  async loadWarehouses(append = false) {
    if (!this.organizationID || this.warehouseLoading || (append && (!this.warehouseHasMore || !this.warehouseCursor))) return;
    this.warehouseLoading = true;
    this.warehouseError = false;
    this.emit();
    try {
      const query = new URLSearchParams({ limit: "50" });
      if (append) query.set("cursor", this.warehouseCursor);
      const page = await this.request<WarehousePage>(`/api/v1/organizations/${this.organizationID}/warehouses?${query}`);
      this.warehouses = this.uniqueAppend(append ? this.warehouses : [], page.warehouses);
      this.warehouseCursor = page.next_cursor ?? "";
      this.warehouseHasMore = page.has_more;
    } catch {
      this.warehouseError = true;
    } finally {
      this.warehouseLoading = false;
      this.emit();
    }
  }

  private uniqueAppend<T extends { id: string }>(current: T[], incoming: T[]) {
    const seen = new Set(current.map((item) => item.id));
    return [...current, ...incoming.filter((item) => !seen.has(item.id) && seen.add(item.id))];
  }

  async select(kind: SelectorKind, value: string) {
    if (value === transferSelectorSentinel) {
      if (kind === "product") return this.loadProducts(true);
      return this.loadWarehouses(true);
    }
    if (value === transferSelectorLoading || value === transferSelectorError || value === "") return;
    if (kind === "product") this.productID = value;
    if (kind === "source") this.sourceID = value;
    if (kind === "destination") this.destinationID = value;
    this.emit();
  }

  options(kind: SelectorKind): Array<Product | Warehouse | { id: string; sku?: string; code?: string; name: string }> {
    const records = kind === "product" ? this.products : this.warehouses;
    const loading = kind === "product" ? this.productLoading : this.warehouseLoading;
    const error = kind === "product" ? this.productError : this.warehouseError;
    const hasMore = kind === "product" ? this.productHasMore : this.warehouseHasMore;
    if (loading) return [...records, { id: transferSelectorLoading, name: "Loading..." }];
    if (error) return [...records, { id: transferSelectorError, name: "Unable to load more" }];
    if (hasMore) return [...records, { id: transferSelectorSentinel, name: "Load more" }];
    return records;
  }

  buildPayload(quantity: string | number): TransferPayload {
    const parsed = Number(quantity);
    const validProduct = this.products.some((item) => item.id === this.productID);
    const validSource = this.warehouses.some((item) => item.id === this.sourceID);
    const validDestination = this.warehouses.some((item) => item.id === this.destinationID);
    if (!validProduct || !validSource || !validDestination || !Number.isInteger(parsed) || parsed <= 0) throw new Error("Choose two different warehouses, a product, and a positive quantity.");
    if (this.sourceID === this.destinationID) throw new Error("Source and destination warehouses must differ.");
    return { source_warehouse_id: this.sourceID, destination_warehouse_id: this.destinationID, items: [{ product_id: this.productID, quantity: parsed }] };
  }

  async submit(quantity: string | number) {
    const payload = this.buildPayload(quantity);
    return this.request<Transfer>(`/api/v1/organizations/${this.organizationID}/transfers`, { method: "POST", headers: { "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify(payload) });
  }
}
