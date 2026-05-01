"use client";
import { useMemo, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useSWRConfig } from "swr";
import Modal from "@/components/ui/Modal";
import ErrorBanner from "@/components/ui/ErrorBanner";
import { endpoints, fetcher } from "@/lib/api/nof1";
import {
  type TraderExecutionModeFilter,
  type TraderRow,
  type TraderStateFilter,
  useTraderStatus,
  useTraders,
} from "@/lib/api/hooks/useTraders";
import type {
  OrderPreviewResponse,
  TraderControlResponse,
} from "@/lib/api/types";
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

type ControlAction = "start" | "pause" | "resume" | "stop";

export default function TradersPanel() {
  const search = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const { mutate } = useSWRConfig();
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
  const [actionBusy, setActionBusy] = useState<ControlAction | null>(null);
  const [actionError, setActionError] = useState<string>("");
  const [actionResult, setActionResult] = useState<TraderControlResponse | null>(
    null,
  );
  const [requestedBy, setRequestedBy] = useState("web-console");
  const [reason, setReason] = useState("");
  const [effectiveUntil, setEffectiveUntil] = useState("");
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewBusy, setPreviewBusy] = useState(false);
  const [previewError, setPreviewError] = useState("");
  const [previewResult, setPreviewResult] =
    useState<OrderPreviewResponse | null>(null);
  const [previewForm, setPreviewForm] = useState({
    side: "buy",
    type: "market",
    symbol: "",
    quantity: "1",
    limitPrice: "",
    decisionId: "",
    correlationId: "",
    riskContext: "",
  });
  const traderListKey = useMemo(
    () =>
      endpoints.traders({
        status: statusFilter,
        executionMode: modeFilter,
        limit: 100,
      }),
    [modeFilter, statusFilter],
  );
  const traderStatusKey = selectedTraderId
    ? endpoints.traderStatus(selectedTraderId)
    : null;
  const selectedControlState = (
    snapshot?.status || selectedTrader?.status || "configured"
  ).toLowerCase();
  const selectedExecutionMode = (
    selectedTrader?.executionMode || ""
  ).toLowerCase();
  const isLiveTrader = selectedExecutionMode === "live";

  async function refreshTraderState() {
    await Promise.all([
      mutate(traderListKey),
      traderStatusKey ? mutate(traderStatusKey) : Promise.resolve(),
    ]);
  }

  async function submitControl(action: ControlAction) {
    if (!selectedTrader) return;
    setActionBusy(action);
    setActionError("");
    setActionResult(null);
    try {
      const payload = await fetcher<TraderControlResponse>(
        endpoints.traderControl(selectedTrader.id, action),
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            trader_id: selectedTrader.id,
            requested_by: requestedBy.trim() || "web-console",
            reason: reason.trim(),
            effective_until:
              action === "pause" && effectiveUntil.trim()
                ? effectiveUntil.trim()
                : undefined,
          }),
        },
      );
      setActionResult(payload);
      await refreshTraderState();
    } catch (error) {
      setActionError(formatError(error));
    } finally {
      setActionBusy(null);
    }
  }

  async function submitPreview() {
    if (!selectedTrader) return;
    setPreviewBusy(true);
    setPreviewError("");
    setPreviewResult(null);
    try {
      const quantity = Number(previewForm.quantity);
      if (!previewForm.symbol.trim()) {
        throw new Error("标的不能为空");
      }
      if (!Number.isFinite(quantity) || quantity <= 0) {
        throw new Error("数量必须为正数");
      }
      const limitPrice = previewForm.limitPrice.trim()
        ? Number(previewForm.limitPrice)
        : undefined;
      if (
        previewForm.limitPrice.trim() &&
        (!Number.isFinite(limitPrice) || (limitPrice ?? 0) <= 0)
      ) {
        throw new Error("限价必须为正数");
      }
      const riskContext = previewForm.riskContext.trim()
        ? JSON.parse(previewForm.riskContext)
        : undefined;
      const payload = await fetcher<OrderPreviewResponse>(
        endpoints.orderPreview(),
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            trader_id: selectedTrader.id,
            decision_id: previewForm.decisionId.trim() || undefined,
            correlation_id: previewForm.correlationId.trim() || undefined,
            orders: [
              {
                symbol: previewForm.symbol.trim().toUpperCase(),
                side: previewForm.side,
                type: previewForm.type,
                quantity,
                ...(limitPrice != null ? { limit_price: limitPrice } : {}),
              },
            ],
            risk_context: riskContext,
          }),
        },
      );
      setPreviewResult(payload);
    } catch (error) {
      setPreviewError(formatError(error));
    } finally {
      setPreviewBusy(false);
    }
  }

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
                className="rounded border px-3 py-3 space-y-3"
                style={{ borderColor: "var(--panel-border)" }}
              >
                <div className="flex items-center justify-between gap-2">
                  <div
                    className="text-[11px] uppercase tracking-[0.12em]"
                    style={{ color: "var(--muted-text)" }}
                  >
                    控制面
                  </div>
                  <div
                    className="text-[11px] tabular-nums"
                    style={{ color: "var(--muted-text)" }}
                  >
                    {selectedExecutionMode || "--"} · {selectedControlState}
                  </div>
                </div>

                <div className="grid gap-2 sm:grid-cols-3">
                  <label className="space-y-1">
                    <div
                      className="text-[11px] uppercase tracking-[0.12em]"
                      style={{ color: "var(--muted-text)" }}
                    >
                      请求人
                    </div>
                    <input
                      className="w-full rounded border px-2 py-1 text-xs"
                      style={{
                        background: "var(--panel-bg)",
                        borderColor: "var(--panel-border)",
                        color: "var(--foreground)",
                      }}
                      value={requestedBy}
                      onChange={(e) => setRequestedBy(e.target.value)}
                      placeholder="web-console"
                    />
                  </label>
                  <label className="space-y-1 sm:col-span-2">
                    <div
                      className="text-[11px] uppercase tracking-[0.12em]"
                      style={{ color: "var(--muted-text)" }}
                    >
                      原因
                    </div>
                    <input
                      className="w-full rounded border px-2 py-1 text-xs"
                      style={{
                        background: "var(--panel-bg)",
                        borderColor: "var(--panel-border)",
                        color: "var(--foreground)",
                      }}
                      value={reason}
                      onChange={(e) => setReason(e.target.value)}
                      placeholder="manual control"
                    />
                  </label>
                </div>

                <div className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                  <label className="space-y-1">
                    <div
                      className="text-[11px] uppercase tracking-[0.12em]"
                      style={{ color: "var(--muted-text)" }}
                    >
                      暂停截止
                    </div>
                    <input
                      className="w-full rounded border px-2 py-1 text-xs"
                      style={{
                        background: "var(--panel-bg)",
                        borderColor: "var(--panel-border)",
                        color: "var(--foreground)",
                      }}
                      value={effectiveUntil}
                      onChange={(e) => setEffectiveUntil(e.target.value)}
                      placeholder="2026-05-02T12:30:00Z"
                    />
                  </label>
                  <div className="flex flex-wrap items-end gap-2">
                    <ControlButton
                      label="启动"
                      disabled={
                        !selectedTrader ||
                        !!actionBusy ||
                        isLiveTrader
                      }
                      busy={actionBusy === "start"}
                      onClick={() => void submitControl("start")}
                    />
                    <ControlButton
                      label="暂停"
                      disabled={!selectedTrader || !!actionBusy}
                      busy={actionBusy === "pause"}
                      onClick={() => void submitControl("pause")}
                    />
                    <ControlButton
                      label="恢复"
                      disabled={!selectedTrader || !!actionBusy || isLiveTrader}
                      busy={actionBusy === "resume"}
                      onClick={() => void submitControl("resume")}
                    />
                    <ControlButton
                      label="停止"
                      disabled={!selectedTrader || !!actionBusy}
                      busy={actionBusy === "stop"}
                      onClick={() => void submitControl("stop")}
                    />
                  </div>
                  <div className="flex items-end justify-end">
                    <button
                      type="button"
                      className="rounded border px-3 py-1 text-xs chip-btn"
                      style={{
                        borderColor: "var(--panel-border)",
                        color: "var(--foreground)",
                      }}
                      onClick={() => setPreviewOpen(true)}
                      disabled={!selectedTrader}
                    >
                      订单预览
                    </button>
                  </div>
                </div>

                {actionError ? (
                  <div
                    className="rounded border px-2 py-2 text-xs"
                    style={{
                      borderColor: "color-mix(in oklab, red 30%, transparent)",
                      background: "color-mix(in oklab, red 10%, transparent)",
                      color: "red",
                    }}
                  >
                    {actionError}
                  </div>
                ) : null}
                {actionResult ? (
                  <div
                    className="rounded border px-2 py-2 text-[11px] leading-relaxed"
                    style={{
                      borderColor: "var(--panel-border)",
                      color: "var(--foreground)",
                      background:
                        "color-mix(in oklab, var(--panel-bg) 88%, black)",
                    }}
                  >
                    <div className="mb-1 text-[11px] uppercase tracking-[0.12em]">
                      控制响应
                    </div>
                    <pre className="whitespace-pre-wrap break-words">
                      {safeJson(actionResult)}
                    </pre>
                  </div>
                ) : null}
                {isLiveTrader ? (
                  <div
                    className="text-[11px]"
                    style={{ color: "var(--muted-text)" }}
                  >
                    实盘控制默认受后端 gate 限制，按钮会得到安全拒绝。
                  </div>
                ) : null}
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

      <Modal
        open={previewOpen}
        onClose={() => setPreviewOpen(false)}
        title={selectedTrader ? `订单预览 • ${selectedTrader.name || selectedTrader.id}` : "订单预览"}
      >
        <div className="space-y-4">
          <div className="grid gap-2 sm:grid-cols-2">
            <label className="space-y-1">
              <div className="text-[11px] uppercase tracking-[0.12em]" style={{ color: "var(--muted-text)" }}>
                标的
              </div>
              <input
                className="w-full rounded border px-2 py-1 text-xs"
                style={{
                  background: "var(--panel-bg)",
                  borderColor: "var(--panel-border)",
                  color: "var(--foreground)",
                }}
                value={previewForm.symbol}
                onChange={(e) =>
                  setPreviewForm((v) => ({ ...v, symbol: e.target.value }))
                }
                placeholder="BTCUSDT"
              />
            </label>
            <label className="space-y-1">
              <div className="text-[11px] uppercase tracking-[0.12em]" style={{ color: "var(--muted-text)" }}>
                决策 ID
              </div>
              <input
                className="w-full rounded border px-2 py-1 text-xs"
                style={{
                  background: "var(--panel-bg)",
                  borderColor: "var(--panel-border)",
                  color: "var(--foreground)",
                }}
                value={previewForm.decisionId}
                onChange={(e) =>
                  setPreviewForm((v) => ({ ...v, decisionId: e.target.value }))
                }
                placeholder="decision-123"
              />
            </label>
            <label className="space-y-1">
              <div className="text-[11px] uppercase tracking-[0.12em]" style={{ color: "var(--muted-text)" }}>
                方向
              </div>
              <select
                className="w-full rounded border px-2 py-1 text-xs"
                style={{
                  background: "var(--panel-bg)",
                  borderColor: "var(--panel-border)",
                  color: "var(--foreground)",
                }}
                value={previewForm.side}
                onChange={(e) =>
                  setPreviewForm((v) => ({ ...v, side: e.target.value }))
                }
              >
                <option value="buy">买入</option>
                <option value="sell">卖出</option>
                <option value="long">做多</option>
                <option value="short">做空</option>
              </select>
            </label>
            <label className="space-y-1">
              <div className="text-[11px] uppercase tracking-[0.12em]" style={{ color: "var(--muted-text)" }}>
                类型
              </div>
              <select
                className="w-full rounded border px-2 py-1 text-xs"
                style={{
                  background: "var(--panel-bg)",
                  borderColor: "var(--panel-border)",
                  color: "var(--foreground)",
                }}
                value={previewForm.type}
                onChange={(e) =>
                  setPreviewForm((v) => ({ ...v, type: e.target.value }))
                }
              >
                <option value="market">市价</option>
                <option value="limit">限价</option>
              </select>
            </label>
            <label className="space-y-1">
              <div className="text-[11px] uppercase tracking-[0.12em]" style={{ color: "var(--muted-text)" }}>
                数量
              </div>
              <input
                className="w-full rounded border px-2 py-1 text-xs"
                style={{
                  background: "var(--panel-bg)",
                  borderColor: "var(--panel-border)",
                  color: "var(--foreground)",
                }}
                value={previewForm.quantity}
                onChange={(e) =>
                  setPreviewForm((v) => ({ ...v, quantity: e.target.value }))
                }
                placeholder="1"
              />
            </label>
            <label className="space-y-1">
              <div className="text-[11px] uppercase tracking-[0.12em]" style={{ color: "var(--muted-text)" }}>
                限价
              </div>
              <input
                className="w-full rounded border px-2 py-1 text-xs"
                style={{
                  background: "var(--panel-bg)",
                  borderColor: "var(--panel-border)",
                  color: "var(--foreground)",
                }}
                value={previewForm.limitPrice}
                onChange={(e) =>
                  setPreviewForm((v) => ({ ...v, limitPrice: e.target.value }))
                }
                placeholder="可选"
              />
            </label>
            <label className="space-y-1 sm:col-span-2">
              <div className="text-[11px] uppercase tracking-[0.12em]" style={{ color: "var(--muted-text)" }}>
                关联 ID
              </div>
              <input
                className="w-full rounded border px-2 py-1 text-xs"
                style={{
                  background: "var(--panel-bg)",
                  borderColor: "var(--panel-border)",
                  color: "var(--foreground)",
                }}
                value={previewForm.correlationId}
                onChange={(e) =>
                  setPreviewForm((v) => ({ ...v, correlationId: e.target.value }))
                }
                placeholder="可选"
              />
            </label>
            <label className="space-y-1 sm:col-span-2">
              <div className="text-[11px] uppercase tracking-[0.12em]" style={{ color: "var(--muted-text)" }}>
                风险上下文 JSON
              </div>
              <textarea
                className="min-h-20 w-full rounded border px-2 py-1 text-xs"
                style={{
                  background: "var(--panel-bg)",
                  borderColor: "var(--panel-border)",
                  color: "var(--foreground)",
                }}
                value={previewForm.riskContext}
                onChange={(e) =>
                  setPreviewForm((v) => ({ ...v, riskContext: e.target.value }))
                }
                placeholder='{"source":"web-console"}'
              />
            </label>
          </div>

          {previewError ? (
            <div
              className="rounded border px-2 py-2 text-xs"
              style={{
                borderColor: "color-mix(in oklab, red 30%, transparent)",
                background: "color-mix(in oklab, red 10%, transparent)",
                color: "red",
              }}
            >
              {previewError}
            </div>
          ) : null}

          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              className="rounded border px-3 py-1 text-xs chip-btn"
              style={{
                borderColor: "var(--panel-border)",
                color: "var(--foreground)",
              }}
              onClick={() => void submitPreview()}
              disabled={previewBusy}
            >
              {previewBusy ? "生成中..." : "生成预览"}
            </button>
            <button
              type="button"
              className="rounded border px-3 py-1 text-xs chip-btn"
              style={{
                borderColor: "var(--panel-border)",
                color: "var(--muted-text)",
              }}
              onClick={() => setPreviewOpen(false)}
            >
              关闭
            </button>
          </div>

          {previewResult ? (
            <div
              className="rounded border px-3 py-2 text-[11px] leading-relaxed"
              style={{
                borderColor: "var(--panel-border)",
                background:
                  "color-mix(in oklab, var(--panel-bg) 88%, black)",
              }}
            >
              <div
                className="mb-1 uppercase tracking-[0.12em]"
                style={{ color: "var(--muted-text)" }}
              >
                预览结果
              </div>
              <pre className="whitespace-pre-wrap break-words">
                {safeJson(previewResult)}
              </pre>
            </div>
          ) : null}
        </div>
      </Modal>
    </div>
  );

  function setQuery(key: string, value: string) {
    const params = new URLSearchParams(search.toString());
    if (value) params.set(key, value);
    else params.delete(key);
    router.replace(`${pathname}?${params.toString()}`);
  }
}

function ControlButton({
  label,
  busy,
  disabled,
  onClick,
}: {
  label: string;
  busy?: boolean;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className="rounded border px-3 py-1 text-xs chip-btn"
      style={{
        borderColor: "var(--panel-border)",
        color: disabled ? "var(--muted-text)" : "var(--foreground)",
      }}
      disabled={disabled}
      onClick={onClick}
      title={label}
    >
      {busy ? `${label}...` : label}
    </button>
  );
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

function formatError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  if (typeof error === "string") {
    return error;
  }
  return "请求失败，请稍后重试。";
}
