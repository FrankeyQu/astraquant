"use client";
import useSWR from "swr";
import { activityAwareRefresh } from "./activityAware";
import { endpoints, fetcher } from "../nof1";
import type { JsonValue } from "../types";

export interface ConversationMessage {
  role: string;
  content: string;
  timestamp?: number | string;
}

export interface ConversationItem {
  model_id: string;
  messages?: ConversationMessage[];
  timestamp?: number | string;
  inserted_at?: number | string;
  cot_trace_summary?: string;
  summary?: string;
  user_prompt?: string;
  cot_trace?: JsonValue;
  llm_response?: JsonValue;
}

export interface ConversationsResponse {
  conversations?: ConversationItem[];
  items?: ConversationItem[];
  logs?: ConversationItem[];
}

export function useConversations() {
  const { data, error, isLoading } = useSWR<ConversationsResponse>(
    endpoints.conversations(),
    fetcher,
    {
      ...activityAwareRefresh(30_000, { hiddenInterval: 90_000 }),
    },
  );
  const items: ConversationItem[] = normalize(data);
  return { items, raw: data, isLoading, isError: !!error };
}

function normalize(data?: ConversationsResponse): ConversationItem[] {
  if (!data) return [];
  if (Array.isArray(data.conversations)) return data.conversations;
  if (Array.isArray(data.items)) return data.items;
  if (Array.isArray(data.logs)) return data.logs;
  return [];
}
