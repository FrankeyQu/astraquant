export type JsonPrimitive = string | number | boolean | null;

export type JsonValue = JsonPrimitive | JsonObject | JsonValue[];

export interface JsonObject {
  [key: string]: JsonValue | undefined;
}

export type AnalyticsTableRow = JsonObject;

export interface ExitPlan {
  profit_target?: number;
  stop_loss?: number;
  invalidation_condition?: string;
}

export interface TraderControlRequest {
  trader_id: string;
  requested_by?: string;
  reason?: string;
  idempotency_key?: string;
  correlation_id?: string;
  effective_until?: string;
}

export interface TraderControlResponse {
  accepted: boolean;
  status: string;
  trader_id: string;
  action: string;
  correlation_id?: string;
  control_state?: string;
  execution_mode?: string;
  queued: boolean;
  control_plane_only: boolean;
  message?: string;
  server_time_ms: number;
}

export interface OrderPreviewOrder {
  symbol: string;
  side: string;
  type: string;
  quantity: number;
  limit_price?: number;
}

export interface OrderPreviewRequest {
  trader_id: string;
  decision_id?: string;
  correlation_id?: string;
  orders: OrderPreviewOrder[];
  risk_context?: unknown;
}

export interface OrderPreviewResponse {
  accepted: boolean;
  status: string;
  preview_id?: string;
  correlation_id?: string;
  checks?: unknown;
  submitted: boolean;
  message?: string;
  server_time_ms: number;
}
