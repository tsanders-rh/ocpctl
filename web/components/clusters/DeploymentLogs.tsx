"use client";

import { useDeploymentLogs } from "@/lib/hooks/useDeploymentLogs";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Download, ArrowDown } from "lucide-react";
import { useState, useRef, useEffect } from "react";
import type { DeploymentLog } from "@/types/api";

interface DeploymentLogsProps {
  clusterId: string;
  jobId?: string;
  clusterStatus?: string;
}

export function DeploymentLogs({
  clusterId,
  jobId,
  clusterStatus,
}: DeploymentLogsProps) {
  const [autoScroll, setAutoScroll] = useState(true);
  const [levelFilter, setLevelFilter] = useState<string | null>(null);
  const [accumulatedLogs, setAccumulatedLogs] = useState<DeploymentLog[]>([]);
  const [lastSequence, setLastSequence] = useState(0);
  const logsEndRef = useRef<HTMLDivElement>(null);

  // Determine if we should poll based on cluster status
  const isActive = clusterStatus === "CREATING" || clusterStatus === "DESTROYING";
  const refreshInterval = isActive ? 2000 : false; // Poll every 2 seconds if active

  const { data, isLoading, error } = useDeploymentLogs(clusterId, {
    jobId,
    afterSequence: lastSequence,
    refreshInterval,
  });

  // Accumulate new logs when data arrives
  useEffect(() => {
    if (data?.logs && data.logs.length > 0) {
      setAccumulatedLogs(prev => [...prev, ...data.logs]);
      const maxSeq = Math.max(...data.logs.map(l => l.sequence));
      setLastSequence(maxSeq);
    }
  }, [data]);

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    if (autoScroll && logsEndRef.current) {
      logsEndRef.current.scrollIntoView({ behavior: "smooth" });
    }
  }, [accumulatedLogs, autoScroll]);

  const handleDownload = () => {
    if (!accumulatedLogs || accumulatedLogs.length === 0) return;

    const content = accumulatedLogs
      .map((log) => {
        const timestamp = new Date(log.timestamp).toLocaleString();
        const level = log.log_level ? `[${log.log_level}]` : "";
        return `[${timestamp}] ${level} ${log.message}`;
      })
      .join("\n");

    const blob = new Blob([content], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `cluster-${clusterId}-logs.txt`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const filteredLogs = accumulatedLogs.filter(
    (log) => !levelFilter || log.log_level === levelFilter
  );

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>Deployment Logs</CardTitle>
            {data?.meta.stats && (
              <p className="text-sm text-muted-foreground mt-1">
                {data.meta.stats.total_lines} lines
                {data.meta.stats.error_count > 0 &&
                  ` • ${data.meta.stats.error_count} errors`}
                {data.meta.stats.warn_count > 0 &&
                  ` • ${data.meta.stats.warn_count} warnings`}
              </p>
            )}
          </div>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() => setAutoScroll(!autoScroll)}
            >
              <ArrowDown
                className={`h-4 w-4 mr-2 ${autoScroll ? "" : "opacity-50"}`}
              />
              Auto-scroll: {autoScroll ? "On" : "Off"}
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={handleDownload}
              disabled={accumulatedLogs.length === 0}
            >
              <Download className="h-4 w-4 mr-2" />
              Download
            </Button>
          </div>
        </div>
        <div className="flex gap-2 mt-3">
          <Badge
            variant={levelFilter === null ? "default" : "outline"}
            className="cursor-pointer"
            onClick={() => setLevelFilter(null)}
          >
            All
          </Badge>
          <Badge
            variant={levelFilter === "error" ? "destructive" : "outline"}
            className="cursor-pointer"
            onClick={() => setLevelFilter("error")}
          >
            Errors ({data?.meta.stats.error_count || 0})
          </Badge>
          <Badge
            variant={levelFilter === "warn" ? "default" : "outline"}
            className="cursor-pointer"
            onClick={() => setLevelFilter("warn")}
          >
            Warnings ({data?.meta.stats.warn_count || 0})
          </Badge>
          <Badge
            variant={levelFilter === "info" ? "default" : "outline"}
            className="cursor-pointer"
            onClick={() => setLevelFilter("info")}
          >
            Info
          </Badge>
        </div>
      </CardHeader>
      <CardContent>
        <div className="bg-black text-green-400 p-4 rounded-md h-[500px] overflow-y-auto font-mono text-sm">
          {isLoading && (
            <div className="text-gray-500">Loading deployment logs...</div>
          )}
          {error && (
            <div className="text-red-500">
              Error loading logs: {error.message}
            </div>
          )}
          {!isLoading && !error && filteredLogs.length === 0 && (
            <div className="text-gray-500">
              No logs available yet. Logs will appear here once the deployment
              starts...
            </div>
          )}
          {filteredLogs.map((log) => (
            <div key={log.sequence} className="mb-1">
              <span className="text-gray-500">
                [{new Date(log.timestamp).toLocaleTimeString()}]
              </span>{" "}
              {log.log_level && (
                <span
                  className={
                    log.log_level === "error"
                      ? "text-red-500"
                      : log.log_level === "warn"
                      ? "text-yellow-500"
                      : "text-green-400"
                  }
                >
                  [{log.log_level}]
                </span>
              )}{" "}
              {log.message}
            </div>
          ))}
          <div ref={logsEndRef} />
        </div>
      </CardContent>
    </Card>
  );
}
