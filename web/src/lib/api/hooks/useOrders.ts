"use client";
import useSWR from "swr";
import { activityAwareRefresh } from "./activityAware";
import { endpoints, fetcher } from "../nof1";

export type OrderStatusFilter = "ALL" | "submitted" | "failed";

export interface OrderRow {
  id: string;
  trader_id: string;
  symbol: string;
  side: string;
  type: string;
  status: string;
  quantity: number;
  limit_price?: number;
  created_at?: string;
  updated_at?: string;
  correlation_id?: string;
  detail?: Record<string, unknown>;
}

type OrdersResponse = {
  orders: OrderRow[];
  meta?: {
    source?: string;
    count?: number;
    limit?: number;
    offset?: number;
    next_offset?: number;
  };
  status: string;
  message?: string;
  server_time_ms: number;
};

export function useOrders(status: OrderStatusFilter = "ALL") {
  const { data, error, isLoading } = useSWR<OrdersResponse>(
    endpoints.orders({ status, limit: 100 }),
    fetcher,
    {
      ...activityAwareRefresh(10_000),
    },
  );

  return {
    orders: data?.orders ?? [],
    meta: data?.meta,
    status: data?.status,
    message: data?.message,
    isLoading,
    isError: !!error,
  };
}

