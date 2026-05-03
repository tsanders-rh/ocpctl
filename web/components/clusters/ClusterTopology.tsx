"use client";

import React, { useMemo } from "react";
import { type Cluster, type ClusterOutputs, Platform } from "@/types/api";
import { useProfile } from "@/lib/hooks/useProfiles";
import { getTopologyLayout, getCanvasDimensions } from "@/lib/utils/topology";
import { TopologyNode } from "./TopologyNode";
import { TopologyLegend } from "./TopologyLegend";

interface ClusterTopologyProps {
  cluster: Cluster;
  outputs?: ClusterOutputs;
}

export function ClusterTopology({ cluster, outputs }: ClusterTopologyProps) {
  // Fetch profile data
  const { data: profile, isLoading: profileLoading, error: profileError } = useProfile(cluster.profile);

  // Calculate layout
  const layout = useMemo(() => {
    if (!cluster) return null;
    return getTopologyLayout(cluster, profile || null, outputs);
  }, [cluster, profile, outputs]);

  const { width, height } = getCanvasDimensions();

  // Loading state
  if (profileLoading) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-center">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary mx-auto"></div>
          <p className="mt-4 text-sm text-muted-foreground">Loading topology...</p>
        </div>
      </div>
    );
  }

  // Error state
  if (profileError || !layout) {
    return (
      <div className="flex items-center justify-center h-96">
        <div className="text-center">
          <p className="text-sm text-destructive">Failed to load cluster topology</p>
          <p className="text-xs text-muted-foreground mt-1">
            {profileError ? "Could not fetch profile data" : "Invalid cluster configuration"}
          </p>
        </div>
      </div>
    );
  }

  // Render topology
  return (
    <div className="w-full">
      {/* Platform and region header */}
      <div className="mb-6 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-foreground">Platform:</span>
            <span className="text-sm text-muted-foreground">
              {layout.platform === Platform.AWS
                ? "Amazon Web Services (AWS)"
                : layout.platform === Platform.GCP
                ? "Google Cloud Platform (GCP)"
                : "IBM Cloud"}
            </span>
          </div>
          <div className="h-4 w-px bg-border" />
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-foreground">Region:</span>
            <span className="text-sm text-muted-foreground">{layout.region}</span>
          </div>
          {layout.cluster_type && (
            <>
              <div className="h-4 w-px bg-border" />
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-foreground">Type:</span>
                <span className="text-sm text-muted-foreground capitalize">{layout.cluster_type}</span>
              </div>
            </>
          )}
        </div>
      </div>

      {/* SVG Topology Diagram */}
      <div className="w-full overflow-auto rounded-lg border bg-background p-4">
        <svg
          viewBox={`0 0 ${width} ${height}`}
          className="w-full h-auto min-h-[600px]"
          role="img"
          aria-label="Cluster architecture diagram"
        >
          {/* SVG Title and Description for accessibility */}
          <title>Cluster Architecture Topology</title>
          <desc>
            Visual representation of {cluster.name} cluster showing control plane, worker nodes,
            load balancers, storage, and network connectivity.
          </desc>

          {/* Background sections for visual grouping */}
          <g opacity="0.05">
            {layout.sections.map((section) => (
              <rect
                key={section.label}
                x="40"
                y={section.y}
                width={width - 80}
                height={section.height}
                rx="8"
                fill={section.color}
              />
            ))}
          </g>

          {/* Section labels */}
          <g className="text-xs">
            {layout.sections.map((section) => (
              <text
                key={`label-${section.label}`}
                x="55"
                y={section.y + 20}
                className="font-semibold text-[15px]"
                fill="rgb(71 85 105)"
              >
                {section.label}
              </text>
            ))}
          </g>

          {/* Define arrow markers for different connection types */}
          <defs>
            <marker
              id="arrowhead-blue"
              markerWidth="8"
              markerHeight="8"
              refX="6"
              refY="3"
              orient="auto"
              markerUnits="strokeWidth"
            >
              <polygon points="0,0 0,6 6,3" fill="rgb(59 130 246)" />
            </marker>
            <marker
              id="arrowhead-green"
              markerWidth="8"
              markerHeight="8"
              refX="6"
              refY="3"
              orient="auto"
              markerUnits="strokeWidth"
            >
              <polygon points="0,0 0,6 6,3" fill="rgb(34 197 94)" />
            </marker>
            <marker
              id="arrowhead-purple"
              markerWidth="8"
              markerHeight="8"
              refX="6"
              refY="3"
              orient="auto"
              markerUnits="strokeWidth"
            >
              <polygon points="0,0 0,6 6,3" fill="rgb(168 85 247)" />
            </marker>
          </defs>

          {/* Render connection lines */}
          <g className="connections">
            {layout.connections.map((connection) => {
              // Color code connections based on their purpose
              let strokeColor = "rgb(59 130 246)"; // blue-500

              if (connection.id.includes("storage")) {
                strokeColor = "rgb(168 85 247)"; // purple-500
              } else if (connection.id.includes("ingress")) {
                strokeColor = "rgb(34 197 94)"; // green-500
              } else if (connection.id.includes("api")) {
                strokeColor = "rgb(59 130 246)"; // blue-500
              }

              // Calculate arrowhead position and angle for data-flow connections
              const dx = connection.to.x - connection.from.x;
              const dy = connection.to.y - connection.from.y;
              const angle = Math.atan2(dy, dx) * 180 / Math.PI;
              const arrowLength = 12;
              const arrowWidth = 8;

              return (
                <g key={connection.id}>
                  <line
                    x1={connection.from.x}
                    y1={connection.from.y}
                    x2={connection.to.x}
                    y2={connection.to.y}
                    stroke={connection.type === "data-flow" ? strokeColor : "rgb(107 114 128)"}
                    strokeWidth={connection.type === "data-flow" ? "2.5" : "2"}
                    strokeOpacity={connection.type === "data-flow" ? "0.7" : "0.5"}
                    strokeLinecap="round"
                    strokeDasharray={connection.type === "dependency" ? "6,4" : undefined}
                  />
                  {connection.type === "data-flow" && (
                    <polygon
                      points={`0,0 -${arrowLength},-${arrowWidth/2} -${arrowLength},${arrowWidth/2}`}
                      fill={strokeColor}
                      opacity="0.8"
                      transform={`translate(${connection.to.x},${connection.to.y}) rotate(${angle})`}
                    />
                  )}
                </g>
              );
            })}
          </g>

          {/* Render topology elements */}
          {layout.elements.map((element) => (
            <TopologyNode
              key={element.id}
              type={element.type}
              label={element.label}
              status={element.status}
              instanceType={element.metadata.instanceType}
              count={element.metadata.count}
              detail={element.metadata.detail || element.metadata.url}
              x={element.position.x}
              y={element.position.y}
              width={element.size.width}
              height={element.size.height}
            />
          ))}
        </svg>
      </div>

      {/* Legend */}
      <TopologyLegend />
    </div>
  );
}
