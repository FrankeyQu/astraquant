"use client";
import { useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import {
  useOrders,
  type OrderRow,
  type OrderStatusFilter,
} from "@/lib/api/hooks/useOrders";
import ErrorBanner from "@/components/ui/ErrorBanner";
import { getModelName, getModelColor } from "@/lib/model/meta";
import { ModelLogoChip } from "@/components/shared/ModelLogo";

const ORDER_STATUS_KEY = "order_status";

export default function OrdersTable() {
  const search = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const statusFilter = normalizeFilter(search.get(ORDER_STATUS_KEY));
  const { orders, meta, status, isLoading, isError } = useOrders(statusFilter);

  const rows = useMemo(() => {
    const arr = [...orders];
    arr.sort(
      (a, b) =>
        dateMs(b.created_at || b.updated_at) -
        dateMs(a.created_at || a.updated_at),
    );
    return arr.slice(0, 100);
  }, [orders]);

  return (
    <div
      className="rounded-md border terminal-text text-[13px] sm:text-xs leading-relaxed"
      style={{
        background: "var(--panel-bg)",
        borderColor: "var(--panel-border)",
      }}
    >
      <div
        className="flex items-center justify-between gap-2 px-3 py-2 border-b"
        style={{ borderColor: "var(--panel-border)" }}
      >
        <div
          className="flex items-center gap-2 text-sm ui-sans"
          style={{ color: "var(--foreground)" }}
        >
          <span className="font-semibold">状态：</span>
          <select
            className="rounded border px-2 py-1 text-xs"
            style={{
              background: "var(--panel-bg)",
              borderColor: "var(--panel-border)",
              color: "var(--foreground)",
            }}
            value={statusFilter}
            onChange={(e) => setQuery(e.target.value as OrderStatusFilter)}
          >
            <option value="ALL">全部</option>
            <option value="submitted">已提交</option>
            <option value="failed">失败</option>
          </select>
        </div>
        <div
          className="text-xs font-semibold tabular-nums ui-sans whitespace-nowrap"
          style={{ color: "var(--muted-text)" }}
        >
          {status || "read_only"} · {meta?.count ?? rows.length}
        </div>
      </div>

      <ErrorBanner
        message={isError ? "订单审计数据源暂时不可用，请稍后重试。" : undefined}
      />

      <div
        className="divide-y"
        style={{
          borderColor:
            "color-mix(in oklab, var(--panel-border) 50%, transparent)",
        }}
      >
        {isLoading ? (
          <div className="p-3 space-y-3">
            <SkeletonOrder />
            <SkeletonOrder />
            <SkeletonOrder />
          </div>
        ) : rows.length ? (
          rows.map((order) => <OrderItem key={order.id} order={order} />)
        ) : (
          <div className="p-3 text-xs" style={{ color: "var(--muted-text)" }}>
            暂无订单审计记录
          </div>
        )}
      </div>
    </div>
  );

  function setQuery(v: OrderStatusFilter) {
    const params = new URLSearchParams(search.toString());
    if (v === "ALL") params.delete(ORDER_STATUS_KEY);
    else params.set(ORDER_STATUS_KEY, v);
    router.replace(`${pathname}?${params.toString()}`);
  }
}

function OrderItem({ order }: { order: OrderRow }) {
  const traderId = order.trader_id || "unknown";
  const symbol = (order.symbol || "--").toUpperCase();
  const side = order.side.toLowerCase();
  const status = order.status.toLowerCase();
  const price =
    order.limit_price && order.limit_price > 0
      ? fmtPrice(order.limit_price)
      : "市价";
  const when = humanTime(order.created_at || order.updated_at);
  const modelColor = getModelColor(traderId);

  return (
    <div className="px-3 py-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div
            className="mb-1 terminal-text text-[13px] sm:text-xs leading-relaxed"
            style={{ color: "var(--foreground)" }}
          >
            <span className="mr-1 align-middle">
              <ModelLogoChip modelId={traderId} size="sm" />
            </span>
            <b style={{ color: modelColor }}>{getModelName(traderId)}</b>
            <span> 提交 </span>
            <b style={{ color: sideColor(side) }}>{sideZh(side)}</b>
            <span> 委托，标的 </span>
            <b>{symbol}</b>
          </div>
        </div>
        <div
          className="text-xs whitespace-nowrap tabular-nums"
          style={{ color: "var(--muted-text)" }}
        >
          {when}
        </div>
      </div>

      <div
        className="mt-1 grid grid-cols-1 gap-0.5 text-[13px] sm:text-xs leading-relaxed sm:grid-cols-2"
        style={{ color: "var(--foreground)" }}
      >
        <div>
          类型：<span className="uppercase">{order.type || "--"}</span>
        </div>
        <div>
          数量：
          <span className="tabular-nums">{fmtNumber(order.quantity, 4)}</span>
        </div>
        <div>价格：{price}</div>
        <div>
          状态：
          <span style={{ color: statusColor(status) }}>{statusZh(status)}</span>
        </div>
      </div>

      {order.correlation_id ? (
        <div
          className="mt-2 text-[11px] tabular-nums truncate"
          style={{ color: "var(--muted-text)" }}
        >
          {shortId(order.correlation_id)}
        </div>
      ) : null}
    </div>
  );
}

function SkeletonOrder() {
  return (
    <div className="space-y-2">
      <div className="h-3 w-3/4 animate-pulse rounded skeleton-bg" />
      <div className="grid grid-cols-2 gap-2">
        <div className="h-3 w-24 animate-pulse rounded skeleton-bg" />
        <div className="h-3 w-20 animate-pulse rounded skeleton-bg" />
        <div className="h-3 w-28 animate-pulse rounded skeleton-bg" />
        <div className="h-3 w-16 animate-pulse rounded skeleton-bg" />
      </div>
    </div>
  );
}

function normalizeFilter(value: string | null): OrderStatusFilter {
  return value === "submitted" || value === "failed" ? value : "ALL";
}

function dateMs(value?: string) {
  if (!value) return 0;
  const t = Date.parse(value);
  return Number.isNaN(t) ? 0 : t;
}

function humanTime(value?: string) {
  const t = dateMs(value);
  if (!t) return "--";
  const d = new Date(t);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fmtPrice(n?: number | null) {
  if (n == null || Number.isNaN(n)) return "--";
  const abs = Math.abs(n);
  const digits = abs >= 1000 ? 1 : abs >= 100 ? 2 : abs >= 1 ? 4 : 5;
  return `$${n.toFixed(digits)}`;
}

function fmtNumber(n?: number | null, digits = 2) {
  if (n == null || Number.isNaN(n)) return "--";
  return n.toLocaleString(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: digits,
  });
}

function sideColor(side: string) {
  return side === "buy" || side === "long"
    ? "#16a34a"
    : side === "sell" || side === "short"
      ? "#ef4444"
      : "var(--muted-text)";
}

function statusColor(status: string) {
  return status === "failed"
    ? "#ef4444"
    : status === "submitted"
      ? "#22c55e"
      : "var(--muted-text)";
}

function sideZh(side: string) {
  if (side === "buy" || side === "long") return "买入";
  if (side === "sell" || side === "short") return "卖出";
  return side || "--";
}

function statusZh(status: string) {
  if (status === "submitted") return "已提交";
  if (status === "failed") return "失败";
  return status || "--";
}

function shortId(value: string) {
  return value.length > 24 ? `${value.slice(0, 12)}...${value.slice(-8)}` : value;
}

