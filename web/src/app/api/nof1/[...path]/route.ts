import { NextRequest, NextResponse } from "next/server";

// Run on the Edge to avoid origin transfer; rely on CDN/browser caching.
export const runtime = "edge";

const UPSTREAM = process.env.NOF1_API_BASE_URL || "http://localhost:8888/api";

// Simple TTL map by first path segment. Tune to trade freshness vs. transfer cost.
// NOTE: With time-aligned polling, s-maxage should match client alignment interval
// to maximize cache hit rates. Most clients align to 10s boundaries.
const TTL_BY_SEGMENT: Record<string, number> = {
  // highly volatile - keep browser cache short, but CDN cache at 10s for alignment
  "crypto-prices": 5,
  // live but not tick-by-tick - align to 10s client polling
  "account-totals": 10,
  positions: 10,
  traders: 10,
  orders: 10,
  "audit-events": 10,
  conversations: 30,
  leaderboard: 60,
  // time-aligned to 10s along with other live-ish endpoints
  trades: 10,
  "since-inception-values": 600,
  analytics: 300,
};

function cacheHeaderFor(pathParts: string[]): string {
  const seg = pathParts[0] || "";
  const ttl = TTL_BY_SEGMENT[seg] ?? 30;

  // For time-aligned endpoints (10s client polling), set s-maxage to match alignment
  // This ensures the first request hits origin, subsequent requests hit Edge cache
  let sMax: number;
  if (
    seg === "crypto-prices" ||
    seg === "account-totals" ||
    seg === "positions" ||
    seg === "traders" ||
    seg === "orders" ||
    seg === "audit-events" ||
    seg === "trades"
  ) {
    // Align CDN cache to 10s boundaries to match client-side time alignment
    sMax = 10;
  } else {
    sMax = Math.max(ttl * 2, 30);
  }

  const swr = Math.max(ttl * 4, 60);
  // Include max-age for browsers so repeated polling hits local cache.
  return `public, max-age=${ttl}, s-maxage=${sMax}, stale-while-revalidate=${swr}`;
}

export async function GET(
  req: NextRequest,
  ctx: { params: Promise<{ path: string[] }> },
) {
  return proxy(req, ctx);
}

export async function POST(
  req: NextRequest,
  ctx: { params: Promise<{ path: string[] }> },
) {
  return proxy(req, ctx);
}

export async function OPTIONS() {
  return new NextResponse(null, {
    headers: {
      "access-control-allow-origin": "*",
      "access-control-allow-methods": "GET,POST,OPTIONS",
      "access-control-allow-headers": "*",
      "cache-control": "public, max-age=3600, s-maxage=3600",
    },
  });
}

async function proxy(
  req: NextRequest,
  ctx: { params: Promise<{ path: string[] }> },
) {
  const { path } = await ctx.params;
  const parts = (path || []).filter(Boolean);
  const subpath = parts.join("/");
  const target = `${UPSTREAM}/${subpath}${req.nextUrl.search}`;
  const method = req.method.toUpperCase();

  const headers = new Headers(req.headers);
  headers.delete("host");
  headers.delete("content-length");
  headers.delete("accept-encoding");
  if (!headers.has("accept")) {
    headers.set("accept", "application/json");
  }

  const upstream = await fetch(target, {
    method,
    headers,
    body: method === "GET" || method === "HEAD" ? undefined : await req.arrayBuffer(),
    cache: "no-store",
  });

  const cacheControl = method === "GET" ? cacheHeaderFor(parts) : "no-store";

  return new NextResponse(upstream.body, {
    status: upstream.status,
    headers: {
      "content-type":
        upstream.headers.get("content-type") ||
        "application/json; charset=utf-8",
      "cache-control": cacheControl,
      "cdn-cache-control": cacheControl,
      "access-control-allow-origin": "*",
      ...(upstream.headers.get("etag")
        ? { etag: upstream.headers.get("etag")! }
        : {}),
      ...(upstream.headers.get("last-modified")
        ? { "last-modified": upstream.headers.get("last-modified")! }
        : {}),
      Vary: "Accept-Encoding",
    },
  });
}
