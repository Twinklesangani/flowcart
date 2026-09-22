export const loadMoreOptionID = "__load_more__";

export type Page<T> = { records: T[]; next_cursor?: string; has_more: boolean };

export function appendUnique<T extends { id: string }>(current: T[], incoming: T[]): T[] {
  const seen = new Set(current.map((item) => item.id));
  return [...current, ...incoming.filter((item) => !seen.has(item.id) && seen.add(item.id))];
}

export function canLoadMore(page: Page<unknown>, loading: boolean) {
  return !loading && page.has_more && Boolean(page.next_cursor);
}

export function resetPage<T>(page: Page<T>): Page<T> {
  return { records: [], next_cursor: undefined, has_more: page.has_more };
}

export function appendSelectorPage<T extends { id: string }>(current: T[], incoming: T[], hasMore: boolean, createSentinel: () => T): T[] {
  const records = appendUnique(current.filter((item) => item.id !== loadMoreOptionID), incoming);
  return hasMore ? [...records, createSentinel()] : records;
}

export function isSelectableOptionID(id: string, loading: boolean) {
  return id !== loadMoreOptionID && id !== "" && !loading;
}