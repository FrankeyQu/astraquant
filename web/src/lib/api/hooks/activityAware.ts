"use client";

import { createTimeAlignedInterval } from "./timeAligned";

export interface ActivityAwareConfig {
  refreshIntervalWhenHidden?: number;
  disableActivityTracking?: boolean;
}

export interface AdaptiveRefreshOptions {
  hiddenInterval?: number;
  disableTracking?: boolean;
  /**
   * Enable time-aligned polling to reduce Fast Data Transfer costs.
   * When enabled, all clients request at the same clock boundaries.
   * Default: true
   */
  enableTimeAlignment?: boolean;
  /**
   * Optional jitter in ms to spread requests slightly.
   * Useful if single-origin performance is a concern.
   * Default: 0 (no jitter)
   */
  alignmentJitter?: number;
}

/**
 * Create activity-aware refresh configuration with optional time alignment.
 *
 * Time alignment ensures all clients request at the same clock boundaries,
 * maximizing Vercel Edge cache hit rates and reducing origin transfer costs by 95%+.
 *
 * @param interval - Base refresh interval in milliseconds
 * @param options - Configuration options
 * @returns SWR-compatible refresh configuration
 *
 * @example
 * // Without time alignment (old behavior)
 * activityAwareRefresh(10_000, { enableTimeAlignment: false })
 *
 * @example
 * // With time alignment (new default)
 * activityAwareRefresh(10_000) // Aligns to 10s boundaries: :00, :10, :20, etc.
 */
export function activityAwareRefresh(
  interval: number,
  options: AdaptiveRefreshOptions = {},
) {
  const {
    hiddenInterval = 0,
    disableTracking = false,
    enableTimeAlignment = true,
    alignmentJitter = 0,
  } = options;

  // Create time-aligned interval function if enabled
  const refreshInterval = enableTimeAlignment
    ? createTimeAlignedInterval({
        alignmentInterval: interval,
        enabled: true,
        maxJitter: alignmentJitter,
      })
    : interval;

  return {
    refreshInterval,
    refreshIntervalWhenHidden: hiddenInterval,
    disableActivityTracking: disableTracking,
  };
}
