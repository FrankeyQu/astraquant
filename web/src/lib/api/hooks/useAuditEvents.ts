"use client";
import useSWR from "swr";
import { activityAwareRefresh } from "./activityAware";
import { endpoints, fetcher } from "../nof1";

export type AuditEventTypeFilter =
  | "ALL"
  | "decision_generated"
  | "decision_validation_failed"
  | "policy_rejected"
  | "approved"
  | "order_submitted"
  | "order_failed";

export interface AuditEventRow {
  id: number;
  type: string;
  trader_id: string;
  cycle_id?: number;
  correlation_id?: string;
  symbol?: string;
  action?: string;
  model_id?: string;
  model_name?: string;
  prompt_digest?: string;
  approval_token_id?: string;
  reason?: string;
  error?: string;
  detail?: unknown;
  created_at: string;
}

type AuditEventsResponse = {
  events: AuditEventRow[];
  meta?: {
    source?: string;
    count?: number;
    limit?: number;
    offset?: number;
    next_offset?: number;
  };
  server_time_ms: number;
};

export function useAuditEvents(type: AuditEventTypeFilter = "ALL") {
  const { data, error, isLoading } = useSWR<AuditEventsResponse>(
    endpoints.auditEvents({ type, limit: 100 }),
    fetcher,
    {
      ...activityAwareRefresh(10_000),
    },
  );

  return {
    events: data?.events ?? [],
    meta: data?.meta,
    isLoading,
    isError: !!error,
  };
}

