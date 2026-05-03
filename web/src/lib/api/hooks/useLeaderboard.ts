"use client";
import useSWR from "swr";
import { activityAwareRefresh } from "./activityAware";
import { endpoints, fetcher } from "../nof1";
import type { JsonObject } from "../types";

export interface LeaderboardRow extends JsonObject {
  id: string; // model_id
  equity: number;
  return_pct?: number;
  num_trades?: number;
  num_wins?: number;
  num_losses?: number;
  sharpe?: number;
  win_rate?: number;
  win_dollars?: number;
  lose_dollars?: number;
  total_pnl?: number;
  total_fees_paid?: number;
  avg_confidence?: number;
  median_confidence?: number;
}

interface LeaderboardResponse {
  leaderboard: LeaderboardRow[];
}

export function useLeaderboard() {
  const { data, error, isLoading } = useSWR<LeaderboardResponse>(
    endpoints.leaderboard?.() ?? "/api/nof1/leaderboard",
    fetcher,
    { ...activityAwareRefresh(60_000, { hiddenInterval: 120_000 }) },
  );
  return { rows: data?.leaderboard ?? [], isLoading, isError: !!error };
}
