import { format, formatDistanceToNow, parseISO } from "date-fns";

export function formatDate(dateString: string): string {
  try {
    const date = parseISO(dateString);
    return format(date, "MMM d, yyyy HH:mm");
  } catch {
    return dateString;
  }
}

export function formatRelativeTime(dateString: string): string {
  try {
    const date = parseISO(dateString);
    return formatDistanceToNow(date, { addSuffix: true });
  } catch {
    return dateString;
  }
}

export function formatTTL(destroyAt: string | null): string {
  if (!destroyAt) return "Never";

  try {
    const date = parseISO(destroyAt);
    const now = new Date();
    const diffMs = date.getTime() - now.getTime();
    const diffHours = Math.floor(diffMs / (1000 * 60 * 60));
    const diffMinutes = Math.floor((diffMs % (1000 * 60 * 60)) / (1000 * 60));

    if (diffHours < 0) return "Expired";
    if (diffHours === 0) return `${diffMinutes}m`;
    if (diffHours < 24) return `${diffHours}h ${diffMinutes}m`;

    const days = Math.floor(diffHours / 24);
    const hours = diffHours % 24;
    return `${days}d ${hours}h`;
  } catch {
    return "Unknown";
  }
}

export function formatCurrency(amount: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount);
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 Bytes";

  const k = 1024;
  const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));

  return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + " " + sizes[i];
}
