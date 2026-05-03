"use client";
import useSWR from "swr";
import { activityAwareRefresh } from "./activityAware";
import { endpoints, fetcher } from "../nof1";

export interface AnalyticsRow {
  id: string;
  model_id?: string;
  fee_pnl_moves_breakdown_table?: {
    total_fees_paid?: number;
    biggest_net_gain?: number;
    biggest_net_loss?: number;
    avg_taker_fee?: number;
    overall_pnl_with_fees?: number;
    overall_pnl_without_fees?: number;
  };
  winners_losers_breakdown_table?: {
    win_rate?: number; // 0-1
    avg_winners_holding_period?: number; // minutes
    avg_losers_holding_period?: number; // minutes
    avg_winners_net_pnl?: number;
    avg_losers_net_pnl?: number;
    avg_winners_notional?: number;
    avg_losers_notional?: number;
  };
  signals_breakdown_table?: {
    total_signals?: number;
    long_signal_pct?: number;
    short_signal_pct?: number;
    hold_signal_pct?: number;
    close_signal_pct?: number;
    pct_mins_flat_combined?: number;
    avg_confidence?: number; // 0-1
    avg_confidence_long?: number;
    avg_confidence_close?: number;
    median_confidence?: number; // 0-1
    avg_leverage?: number; // average effective leverage, if provided by analytics
    median_leverage?: number;
    avg_leverage_long?: number;
  };
  overall_trades_overview_table?: {
    total_trades?: number;
    avg_holding_period_mins?: number;
    median_holding_period_mins?: number;
    std_holding_period_mins?: number;
    avg_convo_leverage?: number; // 用户指定：用于“平均杠杆”的权威口径
    median_convo_leverage?: number;
    avg_size_of_trade_notional?: number;
    median_size_of_trade_notional?: number;
    std_size_of_trade_notional?: number;
  };
  invocation_breakdown_table?: {
    num_invocations?: number;
    avg_invocation_break_mins?: number;
    min_invocation_break_mins?: number;
    max_invocation_break_mins?: number;
  };
  longs_shorts_breakdown_table?: {
    num_long_trades?: number;
    num_short_trades?: number;
    avg_longs_net_pnl?: number;
    avg_shorts_net_pnl?: number;
    avg_longs_holding_period?: number;
    avg_shorts_holding_period?: number;
  };
}

type AnalyticsResponse = { analytics: AnalyticsRow[] };

export function useAnalyticsMap() {
  const { data, error, isLoading } = useSWR<AnalyticsResponse>(
    endpoints.analytics(),
    fetcher,
    { ...activityAwareRefresh(15_000) },
  );
  const map: Record<string, AnalyticsRow> = {};
  for (const r of data?.analytics ?? []) {
    const id = String(r.id || r.model_id || "");
    if (!id) continue;
    map[id] = r;
  }
  return { map, isLoading, isError: !!error };
}
