/**
 * Date/Time Utilities
 *
 * All datetime calculations use UTC to ensure consistency with the backend.
 * The backend sends all timestamps in UTC, and we should compare/calculate using UTC.
 * Display formatting can use local time for user convenience.
 */

// Protobuf timestamp format from gRPC
export interface ProtoTimestamp {
  seconds: number;
  nanos?: number;
}

/**
 * Get current time in UTC milliseconds (epoch time).
 * This is timezone-agnostic and consistent across all clients.
 */
export function nowUTC(): number {
  return Date.now();
}

/**
 * Get current Date object in UTC.
 */
export function nowUTCDate(): Date {
  return new Date();
}

/**
 * Convert a protobuf timestamp, ISO string, or Date to UTC milliseconds.
 * Returns null for invalid/zero timestamps.
 */
export function toUTCMillis(
  ts: ProtoTimestamp | string | Date | number | undefined | null
): number | null {
  if (ts == null) return null;

  // Already milliseconds
  if (typeof ts === "number") {
    // If it looks like seconds (small number), convert to millis
    if (ts > 0 && ts < 10000000000) {
      return ts * 1000;
    }
    return ts > 0 ? ts : null;
  }

  // Date object
  if (ts instanceof Date) {
    const millis = ts.getTime();
    return isNaN(millis) ? null : millis;
  }

  // ISO string
  if (typeof ts === "string") {
    const date = new Date(ts);
    const millis = date.getTime();
    return isNaN(millis) ? null : millis;
  }

  // Protobuf timestamp object
  if (typeof ts === "object" && "seconds" in ts) {
    // Check for zero/sentinel timestamps (e.g., -62135596800 is Go's zero time)
    if (ts.seconds <= 0 || ts.seconds === -62135596800) return null;
    return ts.seconds * 1000 + (ts.nanos || 0) / 1000000;
  }

  return null;
}

/**
 * Convert a protobuf timestamp, ISO string, or number to a Date object.
 * Returns null for invalid/zero timestamps.
 */
export function toDate(
  ts: ProtoTimestamp | string | Date | number | undefined | null
): Date | null {
  const millis = toUTCMillis(ts);
  return millis != null ? new Date(millis) : null;
}

/**
 * Calculate the difference in milliseconds between two timestamps.
 * Both timestamps should be in UTC (which they are if from the backend).
 */
export function diffMillis(
  ts1: ProtoTimestamp | string | Date | number | undefined | null,
  ts2: ProtoTimestamp | string | Date | number | undefined | null
): number | null {
  const millis1 = toUTCMillis(ts1);
  const millis2 = toUTCMillis(ts2);
  if (millis1 == null || millis2 == null) return null;
  return millis1 - millis2;
}

/**
 * Check if a timestamp is older than a given threshold (in milliseconds).
 * Compares against current UTC time.
 */
export function isOlderThan(
  ts: ProtoTimestamp | string | Date | number | undefined | null,
  thresholdMs: number
): boolean {
  const millis = toUTCMillis(ts);
  if (millis == null) return true; // Treat invalid as old
  return nowUTC() - millis > thresholdMs;
}

/**
 * Check if a timestamp is in the future (relative to UTC now).
 */
export function isFuture(
  ts: ProtoTimestamp | string | Date | number | undefined | null
): boolean {
  const millis = toUTCMillis(ts);
  if (millis == null) return false;
  return millis > nowUTC();
}

/**
 * Format a relative time string (e.g., "5m ago", "2h ago").
 * The input timestamp should be UTC (which it is from the backend).
 */
export function formatRelativeTime(
  ts: ProtoTimestamp | string | Date | number | undefined | null,
  options?: { neverLabel?: string; justNowLabel?: string; maxDays?: number }
): string {
  const {
    neverLabel = "Never",
    justNowLabel = "Just now",
    maxDays = 365,
  } = options || {};

  const millis = toUTCMillis(ts);
  if (millis == null) return neverLabel;

  const date = new Date(millis);
  if (isNaN(date.getTime()) || date.getFullYear() < 2000) return neverLabel;

  const diff = nowUTC() - millis;
  const seconds = Math.floor(diff / 1000);

  if (seconds < 0) return justNowLabel; // Future date, treat as now
  if (seconds < 60) return justNowLabel;

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;

  const days = Math.floor(hours / 24);
  if (days > maxDays) return neverLabel;

  return `${days}d ago`;
}

/**
 * Format a timestamp for display using local timezone.
 * Input should be UTC (from backend), output is localized for user display.
 */
export function formatLocalDateTime(
  ts: ProtoTimestamp | string | Date | number | undefined | null,
  fallback = "N/A"
): string {
  const date = toDate(ts);
  if (!date) return fallback;
  return date.toLocaleString();
}

/**
 * Format a timestamp as local date only.
 */
export function formatLocalDate(
  ts: ProtoTimestamp | string | Date | number | undefined | null,
  fallback = "N/A"
): string {
  const date = toDate(ts);
  if (!date) return fallback;
  return date.toLocaleDateString();
}

/**
 * Format a timestamp as local time only.
 */
export function formatLocalTime(
  ts: ProtoTimestamp | string | Date | number | undefined | null,
  fallback = "N/A"
): string {
  const date = toDate(ts);
  if (!date) return fallback;
  return date.toLocaleTimeString();
}

/**
 * Format a timestamp as ISO string (UTC).
 */
export function formatISO(
  ts: ProtoTimestamp | string | Date | number | undefined | null,
  fallback = ""
): string {
  const date = toDate(ts);
  if (!date) return fallback;
  return date.toISOString();
}

/**
 * Parse a protobuf timestamp to ISO string.
 * This is useful when you need a string representation.
 */
export function protoToISO(
  ts: ProtoTimestamp | undefined | null
): string | null {
  if (!ts || ts.seconds <= 0) return null;
  const date = new Date(ts.seconds * 1000 + (ts.nanos || 0) / 1000000);
  return date.toISOString();
}

// Time constants for convenience
export const SECOND_MS = 1000;
export const MINUTE_MS = 60 * SECOND_MS;
export const HOUR_MS = 60 * MINUTE_MS;
export const DAY_MS = 24 * HOUR_MS;
export const WEEK_MS = 7 * DAY_MS;
