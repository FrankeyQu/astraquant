"use client";

import { useEffect, useMemo } from "react";
import useSWR, { useSWRConfig } from "swr";
import { activityAwareRefresh } from "./activityAware";
import { endpoints, fetcher } from "../nof1";

export interface AccountTotalsRow {
  model_id?: string;
  id?: string;
  timestamp: number; // unix seconds
  equity?: number;
  dollar_equity?: number;
  account_value?: number;
  realized_pnl?: number;
  unrealized_pnl?: number;
  total_unrealized_pnl?: number;
  cum_pnl_pct?: number;
  return_pct?: number;
  since_inception_hourly_marker?: number;
  hourly_marker?: number;
  positions?: Record<string, unknown>;
}

type AccountTotalsResponse = { accountTotals: AccountTotalsRow[] };

function markerOf(row: AccountTotalsRow): number | null {
  const raw =
    typeof row.since_inception_hourly_marker === "number"
      ? row.since_inception_hourly_marker
      : typeof row.hourly_marker === "number"
        ? row.hourly_marker
        : null;
  return raw != null ? Number(raw) : null;
}

function sortRows(a: AccountTotalsRow, b: AccountTotalsRow) {
  const ta = Number(a.timestamp ?? 0);
  const tb = Number(b.timestamp ?? 0);
  if (ta !== tb) return ta - tb;
  const ma = markerOf(a) ?? -Infinity;
  const mb = markerOf(b) ?? -Infinity;
  if (ma !== mb) return ma - mb;
  return String(a.model_id || "").localeCompare(String(b.model_id || ""));
}

function rowKey(row: AccountTotalsRow) {
  const id = String(row.model_id ?? row.id ?? "");
  const marker = markerOf(row);
  const ts = Number(row.timestamp ?? 0);
  if (marker != null) return `${id}::marker::${marker}::ts::${ts}`;
  return `${id}::ts::${ts}`;
}

export function useAccountTotals() {
  const baseKey = endpoints.accountTotals();
  const { mutate: globalMutate } = useSWRConfig();

  const {
    data,
    error,
    isLoading,
    isValidating,
    mutate,
  } = useSWR<AccountTotalsResponse>(baseKey, fetcher, {
    refreshInterval: 0,
    revalidateOnFocus: false,
    revalidateIfStale: false,
  });

  const lastMarker = useMemo(() => {
    const rows = data?.accountTotals ?? [];
    let max = -1;
    for (const row of rows) {
      const m = markerOf(row);
      if (m != null && m > max) max = m;
    }
    return max >= 0 ? max : null;
  }, [data]);

  const incKey = useMemo(() => {
    if (lastMarker == null) return null;
    return endpoints.accountTotals(lastMarker);
  }, [lastMarker]);

  useEffect(() => {
    if (typeof window === "undefined" || typeof document === "undefined")
      return;
    if (lastMarker != null) return;

    const ACTIVE_INTERVAL = 10_000;
    const HIDDEN_INTERVAL = 60_000;
    let cancelled = false;
    let timeout: number | undefined;

    const schedule = () => {
      if (cancelled) return;
      const interval =
        document.visibilityState === "visible" ? ACTIVE_INTERVAL : HIDDEN_INTERVAL;
      timeout = window.setTimeout(async () => {
        try {
          await mutate();
        } finally {
          schedule();
        }
      }, interval);
    };

    const handleVisibility = () => {
      if (timeout) window.clearTimeout(timeout);
      schedule();
    };

    document.addEventListener("visibilitychange", handleVisibility);
    schedule();

    return () => {
      cancelled = true;
      if (timeout) window.clearTimeout(timeout);
      document.removeEventListener("visibilitychange", handleVisibility);
    };
  }, [lastMarker, mutate]);

  const {
    data: incData,
    error: incError,
    isValidating: incValidating,
  } = useSWR<AccountTotalsResponse>(incKey, fetcher, {
    ...activityAwareRefresh(10_000),
  });

  // Merge incremental payloads into the base cache
  useEffect(() => {
    const rows = incData?.accountTotals ?? [];
    if (!rows.length) return;
    const baseMarker = lastMarker ?? -1;
    const existing = data?.accountTotals ?? [];
    const highestTimestamp = existing.reduce((max, row) => {
      const ts = Number(row.timestamp ?? 0);
      return ts > max ? ts : max;
    }, -1);

    const candidates = rows.filter((row) => {
      const marker = markerOf(row);
      if (marker != null) {
        if (baseMarker < 0) return true;
        return marker >= baseMarker;
      }
      if (baseMarker < 0) return true;
      const ts = Number(row.timestamp ?? 0);
      if (highestTimestamp < 0) return true;
      return ts >= highestTimestamp;
    });

    if (!candidates.length) return;
    globalMutate<AccountTotalsResponse>(
      baseKey,
      (prev) => {
        const existing = prev?.accountTotals ?? data?.accountTotals ?? [];
        if (!existing.length) {
          const merged = candidates.slice().sort(sortRows);
          return { accountTotals: merged };
        }
        const map = new Map<string, AccountTotalsRow>();
        for (const row of existing) map.set(rowKey(row), row);
        for (const row of candidates) map.set(rowKey(row), row);
        const merged = Array.from(map.values());
        merged.sort(sortRows);
        return { accountTotals: merged };
      },
      { revalidate: false },
    );
  }, [incData, data, baseKey, globalMutate, lastMarker]);

  return {
    data,
    mutate,
    isLoading: isLoading && !data,
    isError: !!(error || incError),
    isValidating: isValidating || incValidating,
  };
}
