import { NextRequest, NextResponse } from "next/server";

// Run on the Edge to avoid origin transfer; rely on CDN/browser caching.
export const runtime = "edge";

const UPSTREAM = process.env.NOF1_API_BASE_URL || "https://nof1.ai/api";

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
  const { path } = await ctx.params;
  const parts = (path || []).filter(Boolean);
  const subpath = parts.join("/");
  const target = `${UPSTREAM}/${subpath}${req.nextUrl.search}`;

  // Forward conditional headers so upstream can 304; minimizes bytes back.
  const passHeaders: Record<string, string> = { Accept: "application/json" };
  const ifNoneMatch = req.headers.get("if-none-match");
  const ifModifiedSince = req.headers.get("if-modified-since");
  if (ifNoneMatch) passHeaders["if-none-match"] = ifNoneMatch;
  if (ifModifiedSince) passHeaders["if-modified-since"] = ifModifiedSince;

  const upstream = await fetch(target, {
    // never cache at the edge fetch layer; rely on response headers we set below
    cache: "no-store",
    headers: passHeaders,
  });

  // Stream the upstream body through without buffering large payloads in memory.
  const res = new NextResponse(upstream.body, {
    status: upstream.status,
    headers: {
      "content-type":
        upstream.headers.get("content-type") ||
        "application/json; charset=utf-8",
      "cache-control": cacheHeaderFor(parts),
      "cdn-cache-control": cacheHeaderFor(parts),
      // Helpful for cross-origin local dev; safe for public data here.
      "access-control-allow-origin": "*",
      // Propagate ETag/Last-Modified when present to enable browser revalidation.
      ...(upstream.headers.get("etag")
        ? { etag: upstream.headers.get("etag")! }
        : {}),
      ...(upstream.headers.get("last-modified")
        ? { "last-modified": upstream.headers.get("last-modified")! }
        : {}),
      Vary: "Accept-Encoding",
    },
  });
  return res;
}

export async function OPTIONS() {
  return new NextResponse(null, {
    headers: {
      "access-control-allow-origin": "*",
      "access-control-allow-methods": "GET,OPTIONS",
      "access-control-allow-headers": "*",
      "cache-control": "public, max-age=3600, s-maxage=3600",
    },
  });
}
