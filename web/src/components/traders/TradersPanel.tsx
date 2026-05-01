"use client";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import ErrorBanner from "@/components/ui/ErrorBanner";
import {
  type TraderExecutionModeFilter,
  type TraderRow,
  type TraderStateFilter,
  useTraderStatus,
  useTraders,
} from "@/lib/api/hooks/useTraders";
import { fmtUSD, pnlClass } from "@/lib/utils/formatters";

const TRADER_STATUS_KEY = "trader_status";
const TRADER_ID_KEY = "trader";
const MODE_KEY = "execution_mode";

const STATUS_OPTIONS: Array<{ value: TraderStateFilter; label: string }> = [
  { value: "ALL", label: "全部" },
  { value: "configured", label: "已配置" },
  { value: "running", label: "运行中" },
  { value: "paused", label: "暂停" },
  { value: "stopped", label: "停止" },
];

const MODE_OPTIONS: Array<{ value: TraderExecutionModeFilter; label: string }> =
  [
    { value: "ALL", label: "全部" },
    { value: "paper", label: "模拟盘" },
    { value: "testnet", label: "测试网" },
    { value: "live", label: "实盘" },
  ];

export default function TradersPanel() {
  const search = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const statusFilter = normalizeStatus(search.get(TRADER_STATUS_KEY));
  const modeFilter = normalizeMode(search.get(MODE_KEY));
  const selectedTraderId = search.get(TRADER_ID_KEY)?.trim() || "";
  const { traders, meta, source, isLoading, isError } = useTraders(
    statusFilter,
    modeFilter,
  );
  const { statusSnapshot, isLoading: isStatusLoading, isError: isStatusError } =
    useTraderStatus(selectedTraderId || undefined);

  const selectedTrader =
    traders.find((trader) => trader.id === selectedTraderId) ?? null;
  const snapshot = statusSnapshot ?? selectedTrader?.statusSnapshot ?? null;

  return (
    <div
      className="rounded-md border terminal-text text-[13px] sm:text-xs leading-relaxed"
      style={{
        background: "var(--panel-bg)",
        borderColor: "var(--panel-border)",
      }}
    >
      <div
        className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 border-b"
        style={{ borderColor: "var(--panel-border)" }}
      >
        <div
          className="flex flex-wrap items-center gap-2 text-sm ui-sans"
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
            onChange={(e) => setQuery(TRADER_STATUS_KEY, e.target.value)}
          >
            {STATUS_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
          <span className="font-semibold">模式：</span>
          <select
            className="rounded border px-2 py-1 text-xs"
            style={{
              background: "var(--panel-bg)",
              borderColor: "var(--panel-border)",
              color: "var(--foreground)",
            }}
            value={modeFilter}
            onChange={(e) => setQuery(MODE_KEY, e.target.value)}
          >
            {MODE_OPTIONS.map((option) => (
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
          {source || "api"} · {meta?.count ?? traders.length}
        </div>
      </div>

      <ErrorBanner
        message={isError ? "交易员数据暂时不可用。" : undefined}
      />

      <div className="grid gap-0 lg:grid-cols-[minmax(0,1.7fr)_minmax(0,1fr)]">
        <div className="min-w-0">
          <div
            className="divide-y"
            style={{
              borderColor:
                "color-mix(in oklab, var(--panel-border) 50%, transparent)",
            }}
          >
            {isLoading ? (
              <div className="p-3 space-y-3">
                <TraderSkeleton />
                <TraderSkeleton />
              </div>
            ) : traders.length ? (
              traders.map((trader) => (
                <TraderItem
                  key={trader.id}
                  trader={trader}
                  selected={trader.id === selectedTraderId}
                  onSelect={() => setQuery(TRADER_ID_KEY, trader.id)}
                />
              ))
            ) : (
              <div className="p-3 text-xs" style={{ color: "var(--muted-text)" }}>
                暂无交易员记录
              </div>
            )}
          </div>
        </div>

        <div
          className="min-w-0 border-t lg:border-t-0 lg:border-l"
          style={{ borderColor: "var(--panel-border)" }}
        >
          <div
            className="flex items-center justify-between gap-2 px-3 py-2 border-b"
            style={{ borderColor: "var(--panel-border)" }}
          >
            <div
              className="text-xs font-semibold uppercase tracking-[0.12em]"
              style={{ color: "var(--muted-text)" }}
            >
              状态快照
            </div>
            <button
              type="button"
              className="text-xs underline-offset-2 hover:underline"
              style={{ color: "var(--muted-text)" }}
              onClick={() => setQuery(TRADER_ID_KEY, "")}
            >
              清除
            </button>
          </div>

          {selectedTraderId ? (
            <div className="space-y-3 px-3 py-3">
              <div className="space-y-1">
                <div
                  className="flex flex-wrap items-center justify-between gap-2"
                  style={{ color: "var(--foreground)" }}
                >
                  <div className="min-w-0">
                    <div className="truncate font-semibold">
                      {selectedTrader?.name || selectedTraderId}
                    </div>
                    <div
                      className="truncate text-[11px]"
                      style={{ color: "var(--muted-text)" }}
                    >
                      {selectedTrader?.id || selectedTraderId}
                    </div>
                  </div>
                  <div
                    className="text-xs tabular-nums whitespace-nowrap"
                    style={{ color: "var(--muted-text)" }}
                  >
                    {statusText(snapshot?.status || selectedTrader?.status)}
                  </div>
                </div>
                <div
                  className="flex flex-wrap gap-x-3 gap-y-1 text-[11px]"
                  style={{ color: "var(--muted-text)" }}
                >
                  <span>
                    模式：
                    <span style={{ color: "var(--foreground)" }}>
                      {modeText(selectedTrader?.executionMode)}
                    </span>
                  </span>
                  <span>
                    提示词摘要：
                    <span style={{ color: "var(--foreground)" }}>
                      {shortText(selectedTrader?.promptDigest)}
                    </span>
                  </span>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-2">
                <InfoCell label="状态" value={statusText(snapshot?.status)} />
                <InfoCell
                  label="运行中"
                  value={snapshot ? (snapshot.isRunning ? "是" : "否") : "--"}
                />
                <InfoCell
                  label="配置版本"
                  value={snapshot ? String(snapshot.activeConfigVersion || 0) : "--"}
                />
                <InfoCell
                  label="更新时间"
                  value={formatTime(snapshot?.updatedAt)}
                />
                <InfoCell
                  label="最近决策"
                  value={formatTime(snapshot?.lastDecisionAt)}
                />
                <InfoCell
                  label="下次决策"
                  value={formatTime(snapshot?.nextDecisionAt)}
                />
                <InfoCell
                  label="暂停截止"
                  value={formatTime(snapshot?.pausedUntil)}
                />
                <InfoCell
                  label="暂停原因"
                  value={shortText(snapshot?.pauseReason)}
                />
              </div>

              <div
                className="rounded border px-2 py-2"
                style={{
                  borderColor: "var(--panel-border)",
                  background:
                    "color-mix(in oklab, var(--panel-bg) 88%, black)",
                }}
              >
                <div
                  className="mb-1 text-[11px] uppercase tracking-[0.12em]"
                  style={{ color: "var(--muted-text)" }}
                >
                  详情
                </div>
                {snapshot ? (
                  snapshot.detail ? (
                    <pre
                      className="max-h-56 overflow-auto whitespace-pre-wrap break-words text-[11px] leading-relaxed"
                      style={{ color: "var(--foreground)" }}
                    >
                      {safeJson(snapshot.detail)}
                    </pre>
                  ) : (
                    <div className="text-xs" style={{ color: "var(--muted-text)" }}>
                      暂无 detail
                    </div>
                  )
                ) : isStatusLoading ? (
                  <div className="text-xs" style={{ color: "var(--muted-text)" }}>
                    加载中...
                  </div>
                ) : isStatusError ? (
                  <div className="text-xs" style={{ color: "var(--muted-text)" }}>
                    状态快照暂时不可用。
                  </div>
                ) : (
                  <div className="text-xs" style={{ color: "var(--muted-text)" }}>
                    暂无 detail
                  </div>
                )}
              </div>
            </div>
          ) : (
            <div className="px-3 py-3 text-xs" style={{ color: "var(--muted-text)" }}>
              请选择一个交易员。
            </div>
          )}
        </div>
      </div>
    </div>
  );

  function setQuery(key: string, value: string) {
    const params = new URLSearchParams(search.toString());
    if (value) params.set(key, value);
    else params.delete(key);
    router.replace(`${pathname}?${params.toString()}`);
  }
}

function TraderItem({
  trader,
  selected,
  onSelect,
}: {
  trader: TraderRow;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className="block w-full px-3 py-3 text-left transition-colors"
      style={{
        background: selected
          ? "color-mix(in oklab, var(--panel-border) 18%, transparent)"
          : "transparent",
      }}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span className="truncate font-semibold" style={{ color: "var(--foreground)" }}>
              {trader.name || "未命名"}
            </span>
            <span
              className="truncate text-[11px]"
              style={{ color: "var(--muted-text)" }}
            >
              {trader.id}
            </span>
          </div>
          <div className="mt-1 text-[11px]" style={{ color: "var(--muted-text)" }}>
            {trader.updatedAt ? `更新时间：${formatTime(trader.updatedAt)}` : "--"}
          </div>
        </div>
        <div className="text-right text-[11px] tabular-nums" style={{ color: "var(--muted-text)" }}>
          {trader.lastDecisionAt ? formatTime(trader.lastDecisionAt) : "--"}
        </div>
      </div>

      <div className="mt-2 grid gap-x-3 gap-y-1 sm:grid-cols-2 xl:grid-cols-4">
        <RowMetric label="状态" value={statusText(trader.status)} />
        <RowMetric label="模式" value={modeText(trader.executionMode)} />
        <RowMetric label="净值" value={fmtUSD(trader.equity)} />
        <RowMetric label="可用现金" value={fmtUSD(trader.availableCash)} />
        <RowMetric
          label="未实现 PnL"
          value={fmtUSD(trader.unrealizedPnl)}
          valueClass={pnlClass(trader.unrealizedPnl)}
        />
        <RowMetric
          label="已实现 PnL"
          value={fmtUSD(trader.realizedPnl)}
          valueClass={pnlClass(trader.realizedPnl)}
        />
        <RowMetric
          label="提示词摘要"
          value={shortText(trader.promptDigest)}
          className="sm:col-span-2 xl:col-span-4"
        />
      </div>
    </button>
  );
}

function RowMetric({
  label,
  value,
  valueClass,
  className = "",
}: {
  label: string;
  value?: string;
  valueClass?: string;
  className?: string;
}) {
  return (
    <div className={className}>
      <div
        className="text-[11px] uppercase tracking-[0.12em]"
        style={{ color: "var(--muted-text)" }}
      >
        {label}
      </div>
      <div
        className={`mt-0.5 truncate text-xs ${valueClass || ""}`}
        title={value}
        style={{ color: "var(--foreground)" }}
      >
        {value || "--"}
      </div>
    </div>
  );
}

function InfoCell({ label, value }: { label: string; value?: string }) {
  return (
    <div
      className="rounded border px-2 py-2"
      style={{ borderColor: "var(--panel-border)" }}
    >
      <div
        className="text-[11px] uppercase tracking-[0.12em]"
        style={{ color: "var(--muted-text)" }}
      >
        {label}
      </div>
      <div className="mt-1 truncate text-xs" style={{ color: "var(--foreground)" }}>
        {value || "--"}
      </div>
    </div>
  );
}

function TraderSkeleton() {
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

function normalizeStatus(value: string | null): TraderStateFilter {
  return value === "configured" ||
    value === "running" ||
    value === "paused" ||
    value === "stopped"
    ? value
    : "ALL";
}

function normalizeMode(value: string | null): TraderExecutionModeFilter {
  return value === "paper" || value === "testnet" || value === "live"
    ? value
    : "ALL";
}

function statusText(status?: string): string {
  switch ((status || "").toLowerCase()) {
    case "configured":
      return "已配置";
    case "running":
      return "运行中";
    case "paused":
      return "暂停";
    case "stopped":
      return "停止";
    case "unknown":
      return "未知";
    default:
      return status || "--";
  }
}

function modeText(mode?: string): string {
  switch ((mode || "").toLowerCase()) {
    case "paper":
      return "模拟盘";
    case "testnet":
      return "测试网";
    case "live":
      return "实盘";
    default:
      return mode || "--";
  }
}

function shortText(value?: string): string {
  if (!value) return "--";
  if (value.length <= 24) return value;
  return `${value.slice(0, 10)}...${value.slice(-8)}`;
}

function formatTime(value?: string): string {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`;
}

function safeJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
