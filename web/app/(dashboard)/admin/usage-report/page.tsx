"use client";

import { Fragment, useState } from "react";
import { format, subDays, startOfMonth, startOfQuarter } from "date-fns";
import { useUsageReport } from "@/lib/hooks/useUsageReport";
import { exportUsageReportPdf } from "@/lib/reports/exportPdf";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatCurrency } from "@/lib/utils/formatters";
import { BarChart } from "@tremor/react";
import { Download, FileText, ChevronRight, ChevronDown } from "lucide-react";
import type { ProfileUsage } from "@/lib/api/endpoints/reports";

const fmt = (d: Date) => format(d, "yyyy-MM-dd");

type Preset = { label: string; range: () => [string, string] };

const PRESETS: Preset[] = [
  { label: "Last 7 days", range: () => [fmt(subDays(new Date(), 7)), fmt(new Date())] },
  { label: "Last 30 days", range: () => [fmt(subDays(new Date(), 30)), fmt(new Date())] },
  { label: "Last 90 days", range: () => [fmt(subDays(new Date(), 90)), fmt(new Date())] },
  { label: "This month", range: () => [fmt(startOfMonth(new Date())), fmt(new Date())] },
  { label: "This quarter", range: () => [fmt(startOfQuarter(new Date())), fmt(new Date())] },
];

export default function UsageReportPage() {
  const [startDate, setStartDate] = useState(fmt(subDays(new Date(), 30)));
  const [endDate, setEndDate] = useState(fmt(new Date()));

  const { data: report, isLoading, error } = useUsageReport(startDate, endDate);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold flex items-center gap-2">
            <FileText className="h-7 w-7" /> Usage Report
          </h1>
          <p className="text-muted-foreground">
            Estimated cost, most used profiles, most active users, and cluster lifecycle stats.
          </p>
        </div>
        <Button
          onClick={() => report && exportUsageReportPdf(report)}
          disabled={!report}
          className="gap-2"
        >
          <Download className="h-4 w-4" /> Download PDF
        </Button>
      </div>

      {/* Range picker */}
      <Card>
        <CardContent className="pt-6 space-y-4">
          <div className="flex flex-wrap gap-2">
            {PRESETS.map((p) => (
              <Button
                key={p.label}
                variant="outline"
                size="sm"
                onClick={() => {
                  const [s, e] = p.range();
                  setStartDate(s);
                  setEndDate(e);
                }}
              >
                {p.label}
              </Button>
            ))}
          </div>
          <div className="flex flex-wrap items-end gap-4">
            <div className="space-y-1">
              <Label htmlFor="start">Start date</Label>
              <Input
                id="start"
                type="date"
                value={startDate}
                max={endDate}
                onChange={(e) => setStartDate(e.target.value)}
                className="w-44"
              />
            </div>
            <div className="space-y-1">
              <Label htmlFor="end">End date</Label>
              <Input
                id="end"
                type="date"
                value={endDate}
                min={startDate}
                onChange={(e) => setEndDate(e.target.value)}
                className="w-44"
              />
            </div>
          </div>
        </CardContent>
      </Card>

      {isLoading && <p className="text-muted-foreground">Loading report…</p>}
      {error && (
        <p className="text-destructive">Failed to load report: {(error as Error).message}</p>
      )}

      {report && (
        <>
          {/* Summary cards */}
          <div className="grid gap-4 md:grid-cols-4">
            <SummaryCard
              title="Total estimated cost"
              value={formatCurrency(report.cost.total_cost)}
              subtitle={
                report.cost.prior_period_comparison
                  ? `${
                      report.cost.prior_period_comparison.percent_change >= 0 ? "▲" : "▼"
                    } ${Math.abs(
                      report.cost.prior_period_comparison.percent_change
                    ).toFixed(1)}% vs prior period`
                  : `${report.cost.total_runtime_hours.toFixed(0)} runtime hrs`
              }
            />
            <SummaryCard
              title="Clusters active"
              value={String(report.cost.clusters_active)}
              subtitle={`${report.lifecycle.created} created · ${report.lifecycle.destroyed} destroyed in range`}
            />
            <SummaryCard
              title="Avg lifetime"
              value={`${report.lifecycle.avg_lifetime_hours.toFixed(1)} hrs`}
              subtitle={`${report.lifecycle.by_status?.HIBERNATED ?? 0} currently hibernated`}
            />
            {(() => {
              const completed =
                report.lifecycle.create_success + report.lifecycle.create_failure;
              return (
                <SummaryCard
                  title="Create success rate"
                  value={completed > 0 ? `${(report.lifecycle.create_success_rate * 100).toFixed(0)}%` : "—"}
                  subtitle={
                    completed > 0
                      ? `${report.lifecycle.create_success}/${completed} create jobs in range`
                      : "no create jobs in range"
                  }
                />
              );
            })()}
          </div>

          {/* Most used profiles */}
          <Card>
            <CardHeader>
              <CardTitle>Most Used Profiles</CardTitle>
              <CardDescription>Ranked by cluster count over the selected range</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {report.profiles.length > 0 && (
                <BarChart
                  data={report.profiles.slice(0, 10)}
                  index="profile"
                  categories={["cluster_count"]}
                  colors={["blue"]}
                  showLegend={false}
                  yAxisWidth={40}
                  className="h-64"
                />
              )}
              <ProfileTable profiles={report.profiles} />
            </CardContent>
          </Card>

          {/* Most active users */}
          <Card>
            <CardHeader>
              <CardTitle>Most Active Users</CardTitle>
              <CardDescription>Ranked by estimated cost over the selected range</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              {report.users.length > 0 && (
                <BarChart
                  data={report.users.slice(0, 10)}
                  index="owner"
                  categories={["estimated_cost"]}
                  colors={["emerald"]}
                  showLegend={false}
                  valueFormatter={(v) => formatCurrency(v)}
                  yAxisWidth={64}
                  className="h-64"
                />
              )}
              <UsageTable
                rows={report.users}
                nameHeader="User"
                nameKey="owner"
                emptyText="No users in range"
              />
            </CardContent>
          </Card>

          {/* Lifecycle breakdowns */}
          <div className="grid gap-4 md:grid-cols-3">
            <BreakdownCard title="By Platform" data={report.lifecycle.by_platform} />
            <BreakdownCard title="By Cluster Type" data={report.lifecycle.by_cluster_type} />
            <BreakdownCard title="By Status" data={report.lifecycle.by_status} />
          </div>
        </>
      )}
    </div>
  );
}

function SummaryCard({
  title,
  value,
  subtitle,
}: {
  title: string;
  value: string;
  subtitle?: string;
}) {
  return (
    <Card>
      <CardContent className="pt-6">
        <p className="text-sm text-muted-foreground">{title}</p>
        <p className="text-2xl font-bold">{value}</p>
        {subtitle && <p className="text-xs text-muted-foreground mt-1">{subtitle}</p>}
      </CardContent>
    </Card>
  );
}

function UsageTable({
  rows,
  nameHeader,
  nameKey,
  emptyText,
}: {
  rows: Array<{
    profile?: string;
    owner?: string;
    cluster_count: number;
    runtime_hours: number;
    estimated_cost: number;
  }>;
  nameHeader: string;
  nameKey: "profile" | "owner";
  emptyText: string;
}) {
  if (rows.length === 0) {
    return <p className="text-sm text-muted-foreground">{emptyText}</p>;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{nameHeader}</TableHead>
          <TableHead className="text-right">Clusters</TableHead>
          <TableHead className="text-right">Runtime hrs</TableHead>
          <TableHead className="text-right">Est. cost</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((r) => (
          <TableRow key={(r[nameKey] as string) || "unknown"}>
            <TableCell className="font-medium">{r[nameKey]}</TableCell>
            <TableCell className="text-right">{r.cluster_count}</TableCell>
            <TableCell className="text-right">{r.runtime_hours.toFixed(1)}</TableCell>
            <TableCell className="text-right">{formatCurrency(r.estimated_cost)}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function ProfileTable({ profiles }: { profiles: ProfileUsage[] }) {
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  if (profiles.length === 0) {
    return <p className="text-sm text-muted-foreground">No profiles in range</p>;
  }

  const toggle = (key: string) =>
    setExpanded((prev) => ({ ...prev, [key]: !prev[key] }));

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Profile</TableHead>
          <TableHead className="text-right">Clusters</TableHead>
          <TableHead className="text-right">Runtime hrs</TableHead>
          <TableHead className="text-right">Est. cost</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {profiles.map((p) => {
          const key = p.profile || "unknown";
          const isOpen = !!expanded[key];
          return (
            <Fragment key={key}>
              <TableRow
                className="cursor-pointer"
                onClick={() => toggle(key)}
              >
                <TableCell className="font-medium">
                  <span className="inline-flex items-center gap-1">
                    {isOpen ? (
                      <ChevronDown className="h-4 w-4 text-muted-foreground" />
                    ) : (
                      <ChevronRight className="h-4 w-4 text-muted-foreground" />
                    )}
                    {p.profile}
                  </span>
                </TableCell>
                <TableCell className="text-right">{p.cluster_count}</TableCell>
                <TableCell className="text-right">{p.runtime_hours.toFixed(1)}</TableCell>
                <TableCell className="text-right">{formatCurrency(p.estimated_cost)}</TableCell>
              </TableRow>
              {isOpen && (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={4} className="bg-muted/30 p-0">
                    <div className="p-3">
                      <Table>
                        <TableHeader>
                          <TableRow>
                            <TableHead>Cluster</TableHead>
                            <TableHead>Owner</TableHead>
                            <TableHead>Region</TableHead>
                            <TableHead>Status</TableHead>
                            <TableHead>Created</TableHead>
                            <TableHead className="text-right">Runtime hrs</TableHead>
                            <TableHead className="text-right">Est. cost</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {p.clusters.map((c) => (
                            <TableRow key={c.name}>
                              <TableCell className="font-medium">{c.name}</TableCell>
                              <TableCell>{c.owner || "—"}</TableCell>
                              <TableCell>{c.region || "—"}</TableCell>
                              <TableCell>{c.status}</TableCell>
                              <TableCell>
                                {format(new Date(c.created_at), "MMM d, yyyy")}
                              </TableCell>
                              <TableCell className="text-right">
                                {c.runtime_hours.toFixed(1)}
                              </TableCell>
                              <TableCell className="text-right">
                                {formatCurrency(c.estimated_cost)}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  </TableCell>
                </TableRow>
              )}
            </Fragment>
          );
        })}
      </TableBody>
    </Table>
  );
}

function BreakdownCard({ title, data }: { title: string; data: Record<string, number> }) {
  const entries = Object.entries(data).sort((a, b) => b[1] - a[1]);
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">No data</p>
        ) : (
          <div className="space-y-2">
            {entries.map(([k, v]) => (
              <div key={k} className="flex justify-between text-sm">
                <span className="text-muted-foreground">{k}</span>
                <span className="font-medium">{v}</span>
              </div>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
