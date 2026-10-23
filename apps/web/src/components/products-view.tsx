"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Product, ProductPage } from "../lib/api";
import { useSession } from "../lib/session";
import { appendUnique } from "../lib/pagination-state";
import { EmptyState, ErrorState, LoadingState } from "./states";

export function ProductsView() {
  const { client, organization } = useSession();
  const [products, setProducts] = useState<Product[]>([]);
  const [loadedOrganizationId, setLoadedOrganizationId] = useState("");
  const [nextCursor, setNextCursor] = useState("");
  const [hasMore, setHasMore] = useState(false);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [loadingMore, setLoadingMore] = useState(false);
  const [continuationError, setContinuationError] = useState(false);
  const generation = useRef(0);

  const load = useCallback(async (cursor = "", append = false, requestGeneration = generation.current) => {
    if (!organization || requestGeneration !== generation.current) return;
    if (append) setLoadingMore(true);
    else setState("loading");
    try {
      const query = new URLSearchParams({ limit: "50" });
      if (cursor) query.set("cursor", cursor);
      const page = await client.request<ProductPage>(`/api/v1/organizations/${organization.id}/products?${query}`);
      if (requestGeneration !== generation.current) return;
      setProducts((current) => append ? appendUnique(current, page.products) : page.products);
      setLoadedOrganizationId(organization.id);
      setNextCursor(page.next_cursor ?? "");
      setHasMore(page.has_more && Boolean(page.next_cursor));
      setContinuationError(false);
      setState("ready");
    } catch {
      if (requestGeneration !== generation.current) return;
      if (append) setContinuationError(true);
      else setState("error");
    } finally {
      if (append && requestGeneration === generation.current) setLoadingMore(false);
    }
  }, [client, organization]);

  useEffect(() => {
    const requestGeneration = ++generation.current;
    const timer = window.setTimeout(() => {
      setProducts([]);
      setNextCursor("");
      setHasMore(false);
      setLoadingMore(false);
      setContinuationError(false);
      void load("", false, requestGeneration);
    }, 0);
    return () => {
      window.clearTimeout(timer);
      generation.current += 1;
    };
  }, [load]);

  if (state === "loading" || (state !== "error" && loadedOrganizationId !== organization?.id)) {
    return <><ProductsHeading /><LoadingState label="Loading products" /></>;
  }
  if (state === "error") {
    return <><ProductsHeading /><ErrorState onRetry={() => void load()} /></>;
  }

  return <><ProductsHeading /><section className="panel">
    {continuationError && <ErrorState message="The next products page could not be loaded." onRetry={() => void load(nextCursor, true)} />}
    {products.length === 0
      ? <EmptyState title="No products yet" message="Products will appear here when they are added to your organization." />
      : <div className="table-wrap"><table><thead><tr><th>Product</th><th>SKU</th></tr></thead><tbody>
        {products.map((product) => <tr key={product.id}><td>{product.name}</td><td>{product.sku}</td></tr>)}
      </tbody></table></div>}
    {hasMore && <button className="secondary-button pagination-button" onClick={() => void load(nextCursor, true)} disabled={loadingMore}>{loadingMore ? "Loading..." : "Load more"}</button>}
  </section></>;
}

function ProductsHeading() {
  return <div className="page-heading"><div><p className="eyebrow">Catalog</p><h1>Products</h1><p className="page-description">Browse the products available to your organization.</p></div></div>;
}
