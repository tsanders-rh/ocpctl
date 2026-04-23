import React from "react";

interface LegendItemProps {
  color: string;
  fillColor: string;
  label: string;
}

function LegendItem({ color, fillColor, label }: LegendItemProps) {
  return (
    <div className="flex items-center gap-2">
      <svg width="24" height="16" className="flex-shrink-0">
        <rect
          x="0"
          y="0"
          width="24"
          height="16"
          rx="3"
          className={`${fillColor} ${color} stroke-2`}
        />
      </svg>
      <span className="text-xs text-muted-foreground">{label}</span>
    </div>
  );
}

export function TopologyLegend() {
  return (
    <div className="mt-6 pt-4 border-t">
      <h4 className="text-sm font-semibold mb-3 text-foreground">Status Legend</h4>
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-4">
        <LegendItem
          color="stroke-emerald-500"
          fillColor="fill-emerald-50 dark:fill-emerald-950"
          label="Ready"
        />
        <LegendItem
          color="stroke-yellow-500"
          fillColor="fill-yellow-50 dark:fill-yellow-950"
          label="Transitioning"
        />
        <LegendItem
          color="stroke-red-500"
          fillColor="fill-red-50 dark:fill-red-950"
          label="Failed"
        />
        <LegendItem
          color="stroke-blue-500"
          fillColor="fill-blue-50 dark:fill-blue-950"
          label="Hibernated"
        />
        <LegendItem
          color="stroke-gray-400"
          fillColor="fill-gray-50 dark:fill-gray-900"
          label="Pending/Destroying"
        />
      </div>
    </div>
  );
}
