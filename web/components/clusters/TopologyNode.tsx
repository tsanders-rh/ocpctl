import React from "react";
import { type ClusterStatus } from "@/types/api";
import { getStatusColor, getStatusFill, type TopologyElementType } from "@/lib/utils/topology";

interface TopologyNodeProps {
  type: TopologyElementType;
  label: string;
  status?: ClusterStatus;
  instanceType?: string;
  count?: string | number;
  detail?: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

// Native SVG icon components
function ServerIcon() {
  return (
    <g className="text-muted-foreground">
      <rect x="0" y="0" width="14" height="14" rx="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <line x1="2" y1="5" x2="12" y2="5" stroke="currentColor" strokeWidth="1.5" />
      <line x1="2" y1="9" x2="12" y2="9" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="4" cy="3" r="0.5" fill="currentColor" />
      <circle cx="4" cy="7" r="0.5" fill="currentColor" />
      <circle cx="4" cy="11" r="0.5" fill="currentColor" />
    </g>
  );
}

function BoxesIcon() {
  return (
    <g className="text-muted-foreground">
      <rect x="0" y="4" width="8" height="8" rx="1" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <rect x="6" y="0" width="8" height="8" rx="1" fill="none" stroke="currentColor" strokeWidth="1.5" />
    </g>
  );
}

function NetworkIcon() {
  return (
    <g className="text-muted-foreground">
      <circle cx="3" cy="3" r="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="11" cy="3" r="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="7" cy="11" r="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <line x1="4.5" y1="4.5" x2="5.5" y2="9.5" stroke="currentColor" strokeWidth="1.5" />
      <line x1="9.5" y1="4.5" x2="8.5" y2="9.5" stroke="currentColor" strokeWidth="1.5" />
    </g>
  );
}

function DatabaseIcon() {
  return (
    <g className="text-muted-foreground">
      <ellipse cx="7" cy="3" rx="6" ry="2" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <path d="M 1 3 L 1 11 C 1 12.1 3.7 13 7 13 C 10.3 13 13 12.1 13 11 L 13 3" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <path d="M 1 7 C 1 8.1 3.7 9 7 9 C 10.3 9 13 8.1 13 7" fill="none" stroke="currentColor" strokeWidth="1.5" />
    </g>
  );
}

function GlobeIcon() {
  return (
    <g className="text-muted-foreground">
      <circle cx="7" cy="7" r="6" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <ellipse cx="7" cy="7" rx="2.5" ry="6" fill="none" stroke="currentColor" strokeWidth="1.5" />
      <line x1="1" y1="7" x2="13" y2="7" stroke="currentColor" strokeWidth="1.5" />
    </g>
  );
}

// Helper to get icon for node type
function NodeIcon({ type }: { type: TopologyElementType }) {
  switch (type) {
    case "control-plane":
    case "managed-control-plane":
      return <ServerIcon />;
    case "worker":
      return <BoxesIcon />;
    case "load-balancer":
      return <NetworkIcon />;
    case "storage":
      return <DatabaseIcon />;
    case "access":
      return <GlobeIcon />;
    default:
      return <ServerIcon />;
  }
}

export const TopologyNode = React.memo(function TopologyNode({
  type,
  label,
  status,
  instanceType,
  count,
  detail,
  x,
  y,
  width,
  height,
}: TopologyNodeProps) {
  const borderColor = status ? getStatusColor(status) : "stroke-gray-300 dark:stroke-gray-600";
  const fillColor = status ? getStatusFill(status) : "fill-gray-50 dark:fill-gray-900";

  // Different styles for different node types
  const isAccess = type === "access";
  const isManagedCP = type === "managed-control-plane";

  return (
    <g transform={`translate(${x}, ${y})`}>
      {/* Background rectangle */}
      <rect
        x={0}
        y={0}
        width={width}
        height={height}
        rx={6}
        className={`${fillColor} ${borderColor} stroke-2`}
      />

      {/* Icon (top-left corner) */}
      {!isAccess && (
        <g transform="translate(8, 8)">
          <NodeIcon type={type} />
        </g>
      )}

      {/* Label text */}
      {label.includes('\n') ? (
        // Multi-line label using tspan
        <text
          x={width / 2}
          y={isAccess ? height / 2 - 12 : height / 2 - 10}
          textAnchor="middle"
          className="fill-foreground text-[13px] font-semibold"
        >
          {label.split('\n').map((line, i) => (
            <tspan key={i} x={width / 2} dy={i === 0 ? 0 : 14}>
              {line}
            </tspan>
          ))}
        </text>
      ) : (
        // Single-line label
        <text
          x={width / 2}
          y={isAccess ? height / 2 - 8 : height / 2 - 6}
          textAnchor="middle"
          className="fill-foreground text-[13px] font-semibold"
        >
          {label}
        </text>
      )}

      {/* Instance type (for control plane and workers) */}
      {instanceType && !isAccess && (
        <text
          x={width / 2}
          y={height / 2 + 6}
          textAnchor="middle"
          className="fill-muted-foreground text-[10px]"
        >
          {instanceType}
        </text>
      )}

      {/* Count (for workers and node groups) */}
      {count && !isAccess && (
        <text
          x={width / 2}
          y={height / 2 + (instanceType ? 18 : 8)}
          textAnchor="middle"
          className="fill-muted-foreground text-[10px]"
        >
          ({count})
        </text>
      )}

      {/* Detail (for managed CP, storage, access) */}
      {detail && !isAccess && (
        <text
          x={width / 2}
          y={height / 2 + (isManagedCP ? 12 : 14)}
          textAnchor="middle"
          className="fill-muted-foreground text-[10px]"
        >
          {detail}
        </text>
      )}

      {/* URL (for access endpoints) */}
      {isAccess && detail && (
        <a href={detail} target="_blank" rel="noopener noreferrer">
          <text
            x={width / 2}
            y={height / 2 + 8}
            textAnchor="middle"
            className="fill-blue-600 hover:fill-blue-800 dark:fill-blue-400 dark:hover:fill-blue-300 text-[10px] font-mono cursor-pointer underline"
          >
            {detail.length > 50 ? `${detail.substring(0, 47)}...` : detail}
          </text>
        </a>
      )}
    </g>
  );
});
