"use client";

import * as React from "react";

import { cn } from "@/lib/utils/cn";

export type BarListItem = {
  name: string;
  value: number;
} & Record<string, unknown>;

type BarListProps = {
  data: BarListItem[];
  /** CSS color for the bar fill (e.g. "hsl(var(--chart-1))"). */
  color?: string;
  valueFormatter?: (value: number) => string;
  className?: string;
};

/**
 * Lightweight horizontal bar list — replaces @tremor/react's <BarList>.
 * Each row shows a label over a proportionally-filled bar with the value
 * aligned to the right.
 */
export function BarList({
  data,
  color = "hsl(var(--chart-1))",
  valueFormatter = (v) => v.toLocaleString(),
  className,
}: BarListProps) {
  const max = React.useMemo(
    () => Math.max(...data.map((d) => d.value), 0),
    [data]
  );

  return (
    <div className={cn("space-y-2", className)}>
      {data.map((item) => {
        const pct = max > 0 ? Math.max((item.value / max) * 100, 2) : 0;
        return (
          <div key={item.name} className="flex items-center gap-3">
            <div className="relative flex-1">
              <div
                className="flex h-8 items-center rounded-sm"
                style={{ width: `${pct}%`, backgroundColor: color, opacity: 0.2 }}
              />
              <span className="absolute inset-y-0 left-2 flex items-center truncate pr-2 text-sm text-foreground">
                {item.name}
              </span>
            </div>
            <span className="w-28 shrink-0 text-right text-sm font-medium tabular-nums text-foreground">
              {valueFormatter(item.value)}
            </span>
          </div>
        );
      })}
    </div>
  );
}
