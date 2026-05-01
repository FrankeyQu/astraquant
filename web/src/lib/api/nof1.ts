import { fetcher } from "./client";

// Always go through our own proxy to avoid CORS issues
const local = (p: string) => `/api/nof1${p}`;

export const endpoints = {
  cryptoPrices: () => local("/crypto-prices"),
  positions: (limit = 1000) => local(`/positions?limit=${limit}`),
  trades: () => local("/trades"),
  traders: (
    params: {
      status?: string;
      executionMode?: string;
      limit?: number;
      offset?: number;
    } = {},
  ) => {
    const query = new URLSearchParams();
    if (params.status && params.status !== "ALL") {
      query.set("status", params.status);
    }
    if (params.executionMode && params.executionMode !== "ALL") {
      query.set("execution_mode", params.executionMode);
    }
    if (params.limit != null) query.set("limit", String(params.limit));
    if (params.offset != null) query.set("offset", String(params.offset));
    const qs = query.toString();
    return local(`/traders${qs ? `?${qs}` : ""}`);
  },
  traderDetail: (traderId: string) =>
    local(`/traders/${encodeURIComponent(traderId)}`),
  traderStatus: (traderId: string) =>
    local(`/traders/${encodeURIComponent(traderId)}/status`),
  orders: (params: { status?: string; limit?: number; offset?: number } = {}) => {
    const query = new URLSearchParams();
    if (params.status && params.status !== "ALL") {
      query.set("status", params.status);
    }
    if (params.limit != null) query.set("limit", String(params.limit));
    if (params.offset != null) query.set("offset", String(params.offset));
    const qs = query.toString();
    return local(`/orders${qs ? `?${qs}` : ""}`);
  },
  auditEvents: (
    params: { type?: string; limit?: number; offset?: number } = {},
  ) => {
    const query = new URLSearchParams();
    if (params.type && params.type !== "ALL") query.set("type", params.type);
    if (params.limit != null) query.set("limit", String(params.limit));
    if (params.offset != null) query.set("offset", String(params.offset));
    const qs = query.toString();
    return local(`/audit-events${qs ? `?${qs}` : ""}`);
  },
  accountTotals: (lastHourlyMarker?: number) =>
    local(
      `/account-totals${lastHourlyMarker != null ? `?lastHourlyMarker=${lastHourlyMarker}` : ""}`,
    ),
  sinceInceptionValues: () => local("/since-inception-values"),
  leaderboard: () => local("/leaderboard"),
  analytics: () => local("/analytics"),
  conversations: () => local("/conversations"),
};

export { fetcher };
