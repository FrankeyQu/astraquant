"use client";
import useSWR from "swr";
import { activityAwareRefresh } from "./activityAware";
import { endpoints, fetcher } from "../nof1";

export type PriceEntry = { symbol: string; price: number; timestamp: number };
type PricesResponse = {
  prices: Record<string, PriceEntry>;
  serverTime: number;
};

export function useCryptoPrices() {
  const { data, error, isLoading } = useSWR<PricesResponse>(
    endpoints.cryptoPrices(),
    fetcher,
    {
      ...activityAwareRefresh(10_000),
    },
  );

  return {
    prices: data?.prices ?? {},
    serverTime: data?.serverTime,
    isLoading,
    isError: !!error,
  };
}
