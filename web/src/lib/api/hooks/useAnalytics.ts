"use client";
import useSWR from "swr";
import { activityAwareRefresh } from "./activityAware";
import { endpoints, fetcher } from "../nof1";
import type { AnalyticsTableRow } from "../types";

export interface AnalyticsResponse {
  serverTime?: number;
  fee_pnl_moves_breakdown_table?: AnalyticsTableRow[];
  winners_losers_breakdown_table?: AnalyticsTableRow[];
  win_rate?: number;
  long_short_trades_ratio?: number;
  avg_confidence?: number;
  median_confidence?: number;
}

export function useAnalytics() {
  const { data, error, isLoading } = useSWR<AnalyticsResponse>(
    endpoints.analytics(),
    fetcher,
    {
      ...activityAwareRefresh(300_000, { hiddenInterval: 600_000 }),
    },
  );
  return { data, isLoading, isError: !!error };
}
