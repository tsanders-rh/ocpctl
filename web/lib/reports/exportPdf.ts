import { jsPDF } from "jspdf";
import autoTable from "jspdf-autotable";
import type { UsageReport } from "@/lib/api/endpoints/reports";
import { formatCurrency } from "@/lib/utils/formatters";

// exportUsageReportPdf builds a data-driven (text-selectable) PDF from a usage
// report. Not a screenshot — each section is rendered as a jspdf-autotable so
// the output stays crisp and copy-pasteable.
export function exportUsageReportPdf(report: UsageReport) {
  const doc = new jsPDF({ orientation: "portrait", unit: "pt", format: "a4" });
  const marginX = 40;
  let y = 48;

  doc.setFontSize(18);
  doc.text("ocpctl Usage Report", marginX, y);
  y += 22;

  doc.setFontSize(10);
  doc.setTextColor(100);
  doc.text(
    `Range: ${report.start_date} to ${report.end_date}   •   Generated: ${new Date(
      report.generated_at
    ).toLocaleString()}`,
    marginX,
    y
  );
  doc.setTextColor(0);
  y += 24;

  // Summary block
  const summaryBody: string[][] = [
    ["Total estimated cost", formatCurrency(report.cost.total_cost)],
  ];
  if (report.cost.prior_period_comparison) {
    const cmp = report.cost.prior_period_comparison;
    summaryBody.push([
      "Prior period cost",
      `${formatCurrency(cmp.previous_period)} (${
        cmp.percent_change >= 0 ? "+" : ""
      }${cmp.percent_change.toFixed(1)}%)`,
    ]);
  }
  autoTable(doc, {
    startY: y,
    head: [["Summary", ""]],
    body: [
      ...summaryBody,
      ["Total runtime hours", report.cost.total_runtime_hours.toFixed(1)],
      ["Clusters active", String(report.cost.clusters_active)],
      ["Clusters created (in range)", String(report.lifecycle.created)],
      ["Clusters destroyed (in range)", String(report.lifecycle.destroyed)],
      ["Currently hibernated", String(report.lifecycle.by_status?.HIBERNATED ?? 0)],
      [
        "Create success rate",
        report.lifecycle.create_success + report.lifecycle.create_failure > 0
          ? `${(report.lifecycle.create_success_rate * 100).toFixed(1)}% (${
              report.lifecycle.create_success
            }/${report.lifecycle.create_success + report.lifecycle.create_failure})`
          : "— (no create jobs in range)",
      ],
      ["Avg cluster lifetime (hrs)", report.lifecycle.avg_lifetime_hours.toFixed(1)],
    ],
    theme: "striped",
    headStyles: { fillColor: [30, 41, 59] },
    margin: { left: marginX, right: marginX },
  });

  // Most used profiles (aggregate)
  autoTable(doc, {
    startY: (doc as any).lastAutoTable.finalY + 20,
    head: [["Most Used Profiles", "Clusters", "Runtime hrs", "Est. cost"]],
    body: report.profiles.map((p) => [
      p.profile,
      String(p.cluster_count),
      p.runtime_hours.toFixed(1),
      formatCurrency(p.estimated_cost),
    ]),
    theme: "striped",
    headStyles: { fillColor: [30, 41, 59] },
    margin: { left: marginX, right: marginX },
  });

  // Per-profile drill-down: the clusters behind each aggregate row.
  for (const p of report.profiles) {
    if (!p.clusters || p.clusters.length === 0) continue;
    autoTable(doc, {
      startY: (doc as any).lastAutoTable.finalY + 16,
      head: [[`Clusters — ${p.profile}`, "Owner", "Region", "Status", "Runtime hrs", "Est. cost"]],
      body: p.clusters.map((c) => [
        c.name,
        c.owner || "—",
        c.region || "—",
        c.status,
        c.runtime_hours.toFixed(1),
        formatCurrency(c.estimated_cost),
      ]),
      theme: "grid",
      styles: { fontSize: 8 },
      headStyles: { fillColor: [71, 85, 105], fontSize: 8 },
      margin: { left: marginX, right: marginX },
    });
  }

  // Most active users
  autoTable(doc, {
    startY: (doc as any).lastAutoTable.finalY + 20,
    head: [["Most Active Users", "Clusters", "Runtime hrs", "Est. cost"]],
    body: report.users.map((u) => [
      u.owner,
      String(u.cluster_count),
      u.runtime_hours.toFixed(1),
      formatCurrency(u.estimated_cost),
    ]),
    theme: "striped",
    headStyles: { fillColor: [30, 41, 59] },
    margin: { left: marginX, right: marginX },
  });

  // Most used OpenShift versions
  autoTable(doc, {
    startY: (doc as any).lastAutoTable.finalY + 20,
    head: [["Most Used OpenShift Versions", "Clusters", "Runtime hrs", "Est. cost"]],
    body: report.versions.map((v) => [
      v.version,
      String(v.cluster_count),
      v.runtime_hours.toFixed(1),
      formatCurrency(v.estimated_cost),
    ]),
    theme: "striped",
    headStyles: { fillColor: [30, 41, 59] },
    margin: { left: marginX, right: marginX },
  });

  // Most used add-ons
  autoTable(doc, {
    startY: (doc as any).lastAutoTable.finalY + 20,
    head: [["Most Used Add-ons", "Clusters", "Runtime hrs", "Est. cost"]],
    body: report.addons.map((a) => [
      a.addon,
      String(a.cluster_count),
      a.runtime_hours.toFixed(1),
      formatCurrency(a.estimated_cost),
    ]),
    theme: "striped",
    headStyles: { fillColor: [30, 41, 59] },
    margin: { left: marginX, right: marginX },
  });

  // Breakdowns
  const breakdownRows = (m: Record<string, number>) =>
    Object.entries(m)
      .sort((a, b) => b[1] - a[1])
      .map(([k, v]) => [k, String(v)]);

  autoTable(doc, {
    startY: (doc as any).lastAutoTable.finalY + 20,
    head: [["Platform", "Clusters"]],
    body: breakdownRows(report.lifecycle.by_platform),
    theme: "striped",
    headStyles: { fillColor: [30, 41, 59] },
    margin: { left: marginX, right: marginX },
  });

  autoTable(doc, {
    startY: (doc as any).lastAutoTable.finalY + 20,
    head: [["Cluster Type", "Clusters"]],
    body: breakdownRows(report.lifecycle.by_cluster_type),
    theme: "striped",
    headStyles: { fillColor: [30, 41, 59] },
    margin: { left: marginX, right: marginX },
  });

  doc.save(`usage-report_${report.start_date}_${report.end_date}.pdf`);
}
