"use client";
import useSWR from "swr";
import { activityAwareRefresh } from "./activityAware";
import { useAccountTotals, type AccountTotalsRow } from "./useAccountTotals";
import { endpoints, fetcher } from "../nof1";

export type TraderStateFilter =
  | "ALL"
  | "configured"
  | "running"
  | "paused"
  | "stopped";

export type TraderExecutionModeFilter = "ALL" | "paper" | "testnet" | "live";

export interface TraderStatusSnapshot {
  traderId: string;
  status: string;
  isRunning: boolean;
  activeConfigVersion: number;
  lastDecisionAt?: string;
  nextDecisionAt?: string;
  pausedUntil?: string;
  pauseReason?: string;
  updatedAt?: string;
  detail?: Record<string, unknown>;
}

export interface TraderRow {
  id: string;
  name: string;
  model: string;
  exchangeProvider: string;
  marketProvider: string;
  executionMode: string;
  orderStyle: string;
  allocationPct: number;
  autoStart: boolean;
  status: string;
  isRunning: boolean;
  activeConfigVersion: number;
  lastDecisionAt?: string;
  nextDecisionAt?: string;
  pausedUntil?: string;
  pauseReason?: string;
  updatedAt?: string;
  promptDigest?: string;
  equity?: number;
  availableCash?: number;
  unrealizedPnl?: number;
  realizedPnl?: number;
  statusSnapshot?: TraderStatusSnapshot;
}

type TraderListMeta = {
  source?: string;
  count?: number;
  limit?: number;
  offset?: number;
  next_offset?: number;
};

type TraderListResponse = {
  traders?: unknown;
  meta?: TraderListMeta;
  server_time_ms?: number;
};

type TraderDetailResponse = {
  trader?: unknown;
  server_time_ms?: number;
};

type TraderStatusResponse = {
  status?: unknown;
  server_time_ms?: number;
};

type TraderDetailRecord = {
  id: string;
  promptDigest?: string;
  statusSnapshot?: TraderStatusSnapshot;
};

type TraderDetailMap = Record<string, TraderDetailRecord>;

export function useTraders(
  status: TraderStateFilter = "ALL",
  executionMode: TraderExecutionModeFilter = "ALL",
) {
  const { data, error, isLoading } = useSWR<TraderListResponse>(
    endpoints.traders({ status, executionMode, limit: 100 }),
    fetcher,
    {
      ...activityAwareRefresh(10_000),
    },
  );

  const traderRows = normalizeTraderRows(data?.traders);
  const { data: totalsData } = useAccountTotals();
  const { detailsById } = useTraderDetails(traderRows.map((row) => row.id));

  const latestTotals = buildLatestAccountTotalsMap(
    totalsData?.accountTotals ?? [],
  );

  const traders = traderRows.map((row) =>
    enrichTraderRow(row, latestTotals[row.id], detailsById[row.id]),
  );

  return {
    traders,
    meta: data?.meta,
    source: data?.meta?.source,
    serverTimeMs: data?.server_time_ms,
    isLoading: isLoading && !data,
    isError: !!error,
  };
}

export function useTraderStatus(traderId?: string) {
  const key = traderId ? endpoints.traderStatus(traderId) : null;
  const { data, error, isLoading } = useSWR<TraderStatusResponse>(
    key,
    fetcher,
    {
      ...activityAwareRefresh(10_000),
    },
  );

  return {
    statusSnapshot: normalizeTraderStatus(data?.status) ?? null,
    serverTimeMs: data?.server_time_ms,
    isLoading,
    isError: !!error,
  };
}

function useTraderDetails(traderIds: string[]) {
  const ids = normalizeIds(traderIds);
  const key = ids.length ? ["trader-details", ids.join("|")] : null;
  const { data, error, isLoading } = useSWR<TraderDetailMap>(
    key,
    async () => {
      const settled = await Promise.allSettled(
        ids.map(async (id) => {
          const payload = await fetcher<TraderDetailResponse>(
            endpoints.traderDetail(id),
          );
          return {
            id,
            detail: normalizeTraderDetail(payload),
          };
        }),
      );

      const map: TraderDetailMap = {};
      for (const item of settled) {
        if (item.status !== "fulfilled") continue;
        const { id, detail } = item.value;
        map[id] = detail;
      }
      return map;
    },
    {
      ...activityAwareRefresh(60_000),
    },
  );

  return {
    detailsById: data ?? {},
    isLoading,
    isError: !!error,
  };
}

function enrichTraderRow(
  row: TraderRow,
  accountSnapshot?: AccountTotalsRow,
  detail?: TraderDetailRecord,
): TraderRow {
  const statusSnapshot = detail?.statusSnapshot ?? row.statusSnapshot;
  const runtimeDetail = statusSnapshot?.detail;
  const allocation = pickRecord(runtimeDetail ?? {}, ["allocation"]);
  const equity =
    pickNumber(accountSnapshot, ["dollar_equity", "equity", "account_value"]) ??
    pickNumber(allocation, ["equity_usd", "equityUsd"]);
  const availableCash =
    pickNumber(allocation, ["available_margin_usd", "availableMarginUsd"]) ??
    computeAvailableCash(accountSnapshot, equity);

  return {
    ...row,
    status: statusSnapshot?.status || row.status,
    isRunning: statusSnapshot?.isRunning ?? row.isRunning,
    activeConfigVersion:
      statusSnapshot?.activeConfigVersion ?? row.activeConfigVersion,
    lastDecisionAt: statusSnapshot?.lastDecisionAt ?? row.lastDecisionAt,
    nextDecisionAt: statusSnapshot?.nextDecisionAt ?? row.nextDecisionAt,
    pausedUntil: statusSnapshot?.pausedUntil ?? row.pausedUntil,
    pauseReason: statusSnapshot?.pauseReason ?? row.pauseReason,
    updatedAt: statusSnapshot?.updatedAt ?? row.updatedAt,
    promptDigest: detail?.promptDigest ?? row.promptDigest,
    equity,
    availableCash,
    unrealizedPnl:
      pickNumber(accountSnapshot, ["unrealized_pnl", "unrealizedPnl"]) ??
      row.unrealizedPnl,
    realizedPnl:
      pickNumber(accountSnapshot, ["realized_pnl", "realizedPnl"]) ??
      row.realizedPnl,
    statusSnapshot,
  };
}

function normalizeTraderRows(rows: unknown): TraderRow[] {
  if (!Array.isArray(rows)) return [];
  const normalized = rows.map(normalizeTraderRow);
  return normalized.filter((row) => row.id);
}

function normalizeTraderRow(value: unknown): TraderRow {
  const row = toRecord(value);
  const statusSnapshot = normalizeTraderStatus(row.status);
  return {
    id:
      pickString(row, ["id", "trader_id", "traderId", "name"]) || "unknown",
    name: pickString(row, ["name", "display_name", "displayName"]) || "",
    model: pickString(row, ["model"]) || "",
    exchangeProvider:
      pickString(row, ["exchange_provider", "exchangeProvider"]) || "",
    marketProvider:
      pickString(row, ["market_provider", "marketProvider"]) || "",
    executionMode:
      pickString(row, ["execution_mode", "executionMode", "mode"]) || "",
    orderStyle: pickString(row, ["order_style", "orderStyle"]) || "",
    allocationPct:
      pickNumber(row, ["allocation_pct", "allocationPct"]) ?? 0,
    autoStart: pickBool(row, ["auto_start", "autoStart"]) ?? false,
    status: statusSnapshot?.status || "unknown",
    isRunning: statusSnapshot?.isRunning ?? false,
    activeConfigVersion: statusSnapshot?.activeConfigVersion ?? 0,
    lastDecisionAt: statusSnapshot?.lastDecisionAt,
    nextDecisionAt: statusSnapshot?.nextDecisionAt,
    pausedUntil: statusSnapshot?.pausedUntil,
    pauseReason: statusSnapshot?.pauseReason,
    updatedAt: statusSnapshot?.updatedAt,
    statusSnapshot,
  };
}

function normalizeTraderDetail(value: unknown): TraderDetailRecord {
  const row = toRecord(value);
  const trader = pickRecord(row, ["trader"]) ?? row;
  const statusSnapshot = normalizeTraderStatus(pickValue(trader, ["status"]));
  return {
    id:
      pickString(trader, ["id", "trader_id", "traderId"]) ||
      statusSnapshot?.traderId ||
      "unknown",
    promptDigest: pickString(trader, ["prompt_digest", "promptDigest"]),
    statusSnapshot,
  };
}

function normalizeTraderStatus(value: unknown): TraderStatusSnapshot | undefined {
  const row = toRecord(value);
  if (!Object.keys(row).length) return undefined;
  return {
    traderId: pickString(row, ["trader_id", "traderId"]) || "",
    status: pickString(row, ["status"]) || "unknown",
    isRunning: pickBool(row, ["is_running", "isRunning"]) ?? false,
    activeConfigVersion:
      pickNumber(row, ["active_config_version", "activeConfigVersion"]) ?? 0,
    lastDecisionAt: pickString(row, ["last_decision_at", "lastDecisionAt"]),
    nextDecisionAt: pickString(row, ["next_decision_at", "nextDecisionAt"]),
    pausedUntil: pickString(row, ["paused_until", "pausedUntil"]),
    pauseReason: pickString(row, ["pause_reason", "pauseReason"]),
    updatedAt: pickString(row, ["updated_at", "updatedAt"]),
    detail: pickRecord(row, ["detail"]),
  };
}

function buildLatestAccountTotalsMap(
  rows: AccountTotalsRow[],
): Record<string, AccountTotalsRow> {
  const latest = new Map<string, AccountTotalsRow>();
  for (const row of rows) {
    const id = normalizeId(row.model_id ?? row.id);
    if (!id) continue;
    const prev = latest.get(id);
    if (!prev || compareAccountTotals(row, prev) >= 0) {
      latest.set(id, row);
    }
  }
  return Object.fromEntries(latest);
}

function compareAccountTotals(
  left: AccountTotalsRow,
  right: AccountTotalsRow,
): number {
  const leftTs = Number(left.timestamp ?? 0);
  const rightTs = Number(right.timestamp ?? 0);
  if (leftTs !== rightTs) return leftTs - rightTs;
  const leftMarker = accountMarker(left);
  const rightMarker = accountMarker(right);
  if (leftMarker !== rightMarker) {
    return (leftMarker ?? -Infinity) - (rightMarker ?? -Infinity);
  }
  return 0;
}

function accountMarker(row: AccountTotalsRow): number | null {
  if (typeof row.since_inception_hourly_marker === "number") {
    return row.since_inception_hourly_marker;
  }
  if (typeof row.hourly_marker === "number") {
    return row.hourly_marker;
  }
  return null;
}

function computeAvailableCash(
  row?: AccountTotalsRow,
  equity?: number,
): number | undefined {
  const accountEquity =
    equity ??
    pickNumber(row, ["dollar_equity", "equity", "account_value"]);
  if (accountEquity == null) return undefined;
  const usedMargin = sumPositionMargin(row?.positions);
  if (usedMargin == null) return undefined;
  return accountEquity - usedMargin;
}

function sumPositionMargin(
  positions?: Record<string, unknown>,
): number | undefined {
  if (!positions) return undefined;
  let total = 0;
  let seen = false;
  for (const value of Object.values(positions)) {
    const margin = pickNumber(toRecord(value), ["margin"]);
    if (margin == null) continue;
    total += margin;
    seen = true;
  }
  return seen ? total : undefined;
}

function normalizeIds(values: string[]): string[] {
  const seen = new Set<string>();
  for (const value of values) {
    const id = normalizeId(value);
    if (id) seen.add(id);
  }
  return [...seen].sort();
}

function normalizeId(value: unknown): string {
  if (typeof value !== "string") return "";
  return value.trim();
}

function toRecord(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return {};
  }
  return value as Record<string, unknown>;
}

function pickValue(row: Record<string, unknown>, keys: string[]): unknown {
  for (const key of keys) {
    if (Object.prototype.hasOwnProperty.call(row, key)) {
      return row[key];
    }
  }
  return undefined;
}

function pickString(
  row: Record<string, unknown>,
  keys: string[],
): string | undefined {
  for (const key of keys) {
    const value = row[key];
    if (typeof value === "string" && value.trim()) {
      return value;
    }
  }
  return undefined;
}

function pickNumber(
  row: object | undefined,
  keys: string[],
): number | undefined {
  if (!row) return undefined;
  const record = row as Record<string, unknown>;
  for (const key of keys) {
    const value = record[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      return value;
    }
    if (typeof value === "string" && value.trim()) {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) {
        return parsed;
      }
    }
  }
  return undefined;
}

function pickBool(
  row: Record<string, unknown>,
  keys: string[],
): boolean | undefined {
  for (const key of keys) {
    const value = row[key];
    if (typeof value === "boolean") return value;
    if (typeof value === "number") return value !== 0;
    if (typeof value === "string") {
      const normalized = value.trim().toLowerCase();
      if (normalized === "true" || normalized === "1") return true;
      if (normalized === "false" || normalized === "0") return false;
    }
  }
  return undefined;
}

function pickRecord(
  row: Record<string, unknown>,
  keys: string[],
): Record<string, unknown> | undefined {
  for (const key of keys) {
    const value = row[key];
    if (typeof value === "object" && value !== null && !Array.isArray(value)) {
      return value as Record<string, unknown>;
    }
  }
  return undefined;
}
