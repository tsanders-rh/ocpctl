"use client";

import { useMemo } from "react";
import { Card } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  ArrowRight,
  Package,
  FileCode,
  FileText,
  Archive,
  AlertCircle,
} from "lucide-react";
import { cn } from "@/lib/utils/cn";

interface DependencyNode {
  id: string;
  name: string;
  type: "operator" | "script" | "manifest" | "helm_chart";
  dependsOn: string[];
  level: number;
}

interface DependencyGraphProps {
  config: {
    operators?: Array<{
      name: string;
      namespace: string;
      dependsOn?: string[];
    }>;
    scripts?: Array<{
      name: string;
      description?: string;
      content?: string;
      path?: string;
      timeout?: string;
      dependsOn?: string[];
    }>;
    manifests?: Array<{
      name: string;
      description?: string;
      content?: string;
      namespace?: string;
      dependsOn?: string[];
    }>;
    helm_charts?: Array<{
      name: string;
      chart: string;
      dependsOn?: string[];
    }>;
  };
}

export function DependencyGraph({ config }: DependencyGraphProps) {
  const { nodes, levels, hasCycle, executionOrder } = useMemo(() => {
    const allNodes: DependencyNode[] = [];

    // Collect all nodes from config
    config.operators?.forEach((op) => {
      allNodes.push({
        id: op.name,
        name: op.name,
        type: "operator",
        dependsOn: op.dependsOn || [],
        level: 0,
      });
    });

    config.scripts?.forEach((script) => {
      allNodes.push({
        id: script.name,
        name: script.name,
        type: "script",
        dependsOn: script.dependsOn || [],
        level: 0,
      });
    });

    config.manifests?.forEach((manifest) => {
      allNodes.push({
        id: manifest.name,
        name: manifest.name,
        type: "manifest",
        dependsOn: manifest.dependsOn || [],
        level: 0,
      });
    });

    config.helm_charts?.forEach((chart) => {
      allNodes.push({
        id: chart.name,
        name: chart.name,
        type: "helm_chart",
        dependsOn: chart.dependsOn || [],
        level: 0,
      });
    });

    // Detect cycles and calculate levels using topological sort
    const visited = new Set<string>();
    const recursionStack = new Set<string>();
    let cycleDetected = false;

    function detectCycle(nodeId: string): boolean {
      if (recursionStack.has(nodeId)) {
        return true;
      }
      if (visited.has(nodeId)) {
        return false;
      }

      visited.add(nodeId);
      recursionStack.add(nodeId);

      const node = allNodes.find((n) => n.id === nodeId);
      if (node) {
        for (const dep of node.dependsOn) {
          if (detectCycle(dep)) {
            return true;
          }
        }
      }

      recursionStack.delete(nodeId);
      return false;
    }

    // Check for cycles
    for (const node of allNodes) {
      if (detectCycle(node.id)) {
        cycleDetected = true;
        break;
      }
    }

    // Calculate levels (topological depth)
    function calculateLevel(nodeId: string, visited = new Set<string>()): number {
      if (visited.has(nodeId)) return 0; // Cycle protection
      visited.add(nodeId);

      const node = allNodes.find((n) => n.id === nodeId);
      if (!node || node.dependsOn.length === 0) {
        return 0;
      }

      const maxDepLevel = Math.max(
        ...node.dependsOn.map((dep) => calculateLevel(dep, new Set(visited)))
      );
      return maxDepLevel + 1;
    }

    allNodes.forEach((node) => {
      node.level = calculateLevel(node.id);
    });

    // Group by levels
    const levelMap = new Map<number, DependencyNode[]>();
    allNodes.forEach((node) => {
      if (!levelMap.has(node.level)) {
        levelMap.set(node.level, []);
      }
      levelMap.get(node.level)!.push(node);
    });

    const sortedLevels = Array.from(levelMap.entries()).sort(
      ([a], [b]) => a - b
    );

    // Calculate execution order
    const order = sortedLevels.flatMap(([_, nodes]) =>
      nodes.map((n) => ({ name: n.name, type: n.type }))
    );

    return {
      nodes: allNodes,
      levels: sortedLevels,
      hasCycle: cycleDetected,
      executionOrder: order,
    };
  }, [config]);

  const getIcon = (type: DependencyNode["type"]) => {
    switch (type) {
      case "operator":
        return Package;
      case "script":
        return FileCode;
      case "manifest":
        return FileText;
      case "helm_chart":
        return Archive;
    }
  };

  const getTypeColor = (type: DependencyNode["type"]) => {
    switch (type) {
      case "operator":
        return "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300";
      case "script":
        return "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300";
      case "manifest":
        return "bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-300";
      case "helm_chart":
        return "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-300";
    }
  };

  if (nodes.length === 0) {
    return (
      <Card className="p-8 text-center">
        <p className="text-muted-foreground">
          No components configured for this addon.
        </p>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      {/* Cycle Warning */}
      {hasCycle && (
        <Card className="p-4 border-destructive bg-destructive/10">
          <div className="flex items-center gap-2 text-destructive">
            <AlertCircle className="h-5 w-5" />
            <div>
              <p className="font-semibold">Circular Dependency Detected</p>
              <p className="text-sm">
                This addon contains circular dependencies which may cause
                deployment issues.
              </p>
            </div>
          </div>
        </Card>
      )}

      {/* Dependency Graph Visualization */}
      <Card className="p-6">
        <h3 className="text-lg font-semibold mb-4">Dependency Graph</h3>
        <div className="space-y-8">
          {levels.map(([level, levelNodes], idx) => (
            <div key={level}>
              {/* Level Header */}
              <div className="flex items-center gap-2 mb-3">
                <Badge variant="outline" className="text-xs">
                  Level {level}
                </Badge>
                <span className="text-xs text-muted-foreground">
                  {level === 0
                    ? "No dependencies"
                    : `Depends on level ${level - 1}`}
                </span>
              </div>

              {/* Nodes at this level */}
              <div className="flex flex-wrap gap-3 ml-4">
                {levelNodes.map((node) => {
                  const Icon = getIcon(node.type);
                  return (
                    <NodeCard
                      key={node.id}
                      node={node}
                      icon={Icon}
                      colorClass={getTypeColor(node.type)}
                      allNodes={nodes}
                    />
                  );
                })}
              </div>

              {/* Arrow to next level */}
              {idx < levels.length - 1 && (
                <div className="flex justify-center my-4">
                  <ArrowRight className="h-5 w-5 text-muted-foreground rotate-90" />
                </div>
              )}
            </div>
          ))}
        </div>
      </Card>

      {/* Execution Order */}
      <Card className="p-6">
        <h3 className="text-lg font-semibold mb-4">Execution Order</h3>
        <div className="space-y-2">
          {executionOrder.map((item, idx) => {
            const Icon = getIcon(item.type);
            return (
              <div
                key={`${item.name}-${idx}`}
                className="flex items-center gap-3 p-3 rounded-md bg-muted/50"
              >
                <Badge variant="secondary" className="text-xs w-12 justify-center">
                  {idx + 1}
                </Badge>
                <Icon className="h-4 w-4 text-muted-foreground" />
                <span className="font-medium">{item.name}</span>
                <Badge className={cn("text-xs ml-auto", getTypeColor(item.type))}>
                  {item.type.replace("_", " ")}
                </Badge>
              </div>
            );
          })}
        </div>
      </Card>
    </div>
  );
}

// Node Card Component
function NodeCard({
  node,
  icon: Icon,
  colorClass,
  allNodes,
}: {
  node: DependencyNode;
  icon: any;
  colorClass: string;
  allNodes: DependencyNode[];
}) {
  const dependencies = node.dependsOn
    .map((dep) => allNodes.find((n) => n.id === dep))
    .filter(Boolean);

  return (
    <Card className="p-4 min-w-[200px] hover:bg-muted/50 transition-colors">
      <div className="space-y-2">
        {/* Header */}
        <div className="flex items-start gap-2">
          <Icon className="h-4 w-4 text-muted-foreground mt-0.5" />
          <div className="flex-1 min-w-0">
            <p className="font-medium text-sm truncate">{node.name}</p>
            <Badge className={cn("text-xs mt-1", colorClass)}>
              {node.type.replace("_", " ")}
            </Badge>
          </div>
        </div>

        {/* Dependencies */}
        {dependencies.length > 0 && (
          <div className="pt-2 border-t">
            <p className="text-xs text-muted-foreground mb-1">Depends on:</p>
            <div className="space-y-1">
              {dependencies.map((dep) => (
                <div
                  key={dep!.id}
                  className="text-xs flex items-center gap-1"
                >
                  <ArrowRight className="h-3 w-3 text-muted-foreground" />
                  <span className="truncate">{dep!.name}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </Card>
  );
}
