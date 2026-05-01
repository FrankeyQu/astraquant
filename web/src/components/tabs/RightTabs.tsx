"use client";
import { useSearchParams } from "next/navigation";
import { PositionsPanel } from "@/components/tabs/PositionsPanel";
import TradesTable from "@/components/trades/TradesTable";
import OrdersTable from "@/components/orders/OrdersTable";
import AuditEventsPanel from "@/components/audit/AuditEventsPanel";
import AnalyticsPanel from "@/components/analytics/AnalyticsPanel";
import ReadmePanel from "@/components/tabs/ReadmePanel";
import ModelChatPanel from "@/components/chat/ModelChatPanel";

export default function RightTabs() {
  const search = useSearchParams();
  const tab = search.get("tab") || "positions";
  if (tab === "chat") return <ModelChatPanel />;
  if (tab === "trades") return <TradesTable />;
  if (tab === "orders") return <OrdersTable />;
  if (tab === "audit") return <AuditEventsPanel />;
  if (tab === "analytics") return <AnalyticsPanel />;
  if (tab === "readme") return <ReadmePanel />;
  return <PositionsPanel />;
}
