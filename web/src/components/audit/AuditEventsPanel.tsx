"use client";
import { useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import {
  useAuditEvents,
  type AuditEventRow,
  type AuditEventTypeFilter,
} from "@/lib/api/hooks/useAuditEvents";
import ErrorBanner from "@/components/ui/ErrorBanner";
import { getModelColor, getModelName } from "@/lib/model/meta";
import { ModelLogoChip } from "@/components/shared/ModelLogo";

const AUDIT_TYPE_KEY = "audit_type";

const TYPE_OPTIONS: { value: AuditEventTypeFilter; label: string }[] = [
  { value: "ALL", label: "全部" },
  { value: "decision_generated", label: "决策" },
  { value: "decision_validation_failed", label: "校验失败" },
  { value: "policy_rejected", label: "策略拒绝" },
  { value: "approved", label: "已批准" },
  { value: "order_submitted", label: "订单提交" },
  { value: "order_failed", label: "订单失败" },
];

export default function AuditEventsPanel() {
  const search = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const typeFilter = normalizeType(search.get(AUDIT_TYPE_KEY));
  const { events, meta, isLoading, isError } = useAuditEvents(typeFilter);

  const rows = useMemo(() => {
    const arr = [...events];
    arr.sort((a, b) => dateMs(b.created_at) - dateMs(a.created_at));
    return arr.slice(0, 100);
  }, [events]);

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
          <span className="font-semibold">类型：</span>
          <select
            className="rounded border px-2 py-1 text-xs"
            style={{
              background: "var(--panel-bg)",
              borderColor: "var(--panel-border)",
              color: "var(--foreground)",
            }}
            value={typeFilter}
            onChange={(e) => setQuery(e.target.value as AuditEventTypeFilter)}
          >
            {TYPE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </div>
        <div
          className="text-xs font-semibold tabular-nums ui-sans whitespace-nowrap"
          style={{ color: "var(--muted-text)" }}
        >
          {meta?.source || "audit_event_repo"} · {meta?.count ?? rows.length}
        </div>
      </div>

      <ErrorBanner
        message={isError ? "审计事件数据源暂时不可用，请稍后重试。" : undefined}
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
            <SkeletonAudit />
            <SkeletonAudit />
            <SkeletonAudit />
          </div>
        ) : rows.length ? (
          rows.map((event) => <AuditEventItem key={event.id} event={event} />)
        ) : (
          <div className="p-3 text-xs" style={{ color: "var(--muted-text)" }}>
            暂无审计事件
          </div>
        )}
      </div>
    </div>
  );

  function setQuery(v: AuditEventTypeFilter) {
    const params = new URLSearchParams(search.toString());
    if (v === "ALL") params.delete(AUDIT_TYPE_KEY);
    else params.set(AUDIT_TYPE_KEY, v);
    router.replace(`${pathname}?${params.toString()}`);
  }
}

function AuditEventItem({ event }: { event: AuditEventRow }) {
  const actorId = event.model_id || event.trader_id || "system";
  const modelColor = getModelColor(actorId);
  const title = eventTitle(event);
  const detail = event.error || event.reason || event.action || event.symbol || "";

  return (
    <div className="px-3 py-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div
            className="mb-1 terminal-text text-[13px] sm:text-xs leading-relaxed"
            style={{ color: "var(--foreground)" }}
          >
            <span className="mr-1 align-middle">
              <ModelLogoChip modelId={actorId} size="sm" />
            </span>
            <b style={{ color: modelColor }}>
              {event.model_name || getModelName(actorId)}
            </b>
            <span> · </span>
            <b style={{ color: typeColor(event.type) }}>{title}</b>
            {event.symbol ? (
              <>
                <span> · </span>
                <b>{event.symbol.toUpperCase()}</b>
              </>
            ) : null}
          </div>
          {detail ? (
            <div
              className="truncate text-[12px]"
              style={{ color: "var(--muted-text)" }}
            >
              {detail}
            </div>
          ) : null}
        </div>
        <div
          className="text-xs whitespace-nowrap tabular-nums"
          style={{ color: "var(--muted-text)" }}
        >
          {humanTime(event.created_at)}
        </div>
      </div>

      <div
        className="mt-2 grid grid-cols-1 gap-0.5 text-[12px] sm:grid-cols-2"
        style={{ color: "var(--muted-text)" }}
      >
        <div className="truncate">事件：{event.type || "--"}</div>
        <div className="truncate">交易员：{event.trader_id || "--"}</div>
        <div className="truncate">周期：{event.cycle_id ?? "--"}</div>
        <div className="truncate">
          关联：{event.correlation_id ? shortId(event.correlation_id) : "--"}
        </div>
      </div>
    </div>
  );
}

function SkeletonAudit() {
  return (
    <div className="space-y-2">
      <div className="h-3 w-4/5 animate-pulse rounded skeleton-bg" />
      <div className="h-3 w-2/3 animate-pulse rounded skeleton-bg" />
      <div className="grid grid-cols-2 gap-2">
        <div className="h-3 w-28 animate-pulse rounded skeleton-bg" />
        <div className="h-3 w-24 animate-pulse rounded skeleton-bg" />
      </div>
    </div>
  );
}

function normalizeType(value: string | null): AuditEventTypeFilter {
  return TYPE_OPTIONS.some((option) => option.value === value)
    ? (value as AuditEventTypeFilter)
    : "ALL";
}

function eventTitle(event: AuditEventRow) {
  switch (event.type) {
    case "decision_generated":
      return "生成决策";
    case "decision_validation_failed":
      return "决策校验失败";
    case "policy_rejected":
      return "策略拒绝";
    case "approved":
      return "批准";
    case "order_submitted":
      return "订单已提交";
    case "order_failed":
      return "订单失败";
    default:
      return event.type || "审计事件";
  }
}

function typeColor(type: string) {
  if (type === "order_failed" || type === "decision_validation_failed") {
    return "#ef4444";
  }
  if (type === "policy_rejected") return "#f59e0b";
  if (type === "order_submitted" || type === "approved") return "#22c55e";
  if (type === "decision_generated") return "#38bdf8";
  return "var(--muted-text)";
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

function shortId(value: string) {
  return value.length > 24
    ? `${value.slice(0, 12)}...${value.slice(-8)}`
    : value;
}

