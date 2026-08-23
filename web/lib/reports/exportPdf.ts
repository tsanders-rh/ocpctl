import { jsPDF } from "jspdf";
import autoTable from "jspdf-autotable";
import type { UsageReport } from "@/lib/api/endpoints/reports";
import { formatCurrency } from "@/lib/utils/formatters";

// drawBarChart renders a simple horizontal bar chart directly with jsPDF
// primitives (crisp vector, no rasterization) from the report data. Draws up to
// the top 10 items and returns the Y coordinate just below the chart so the
// caller can position the table that follows. Adds a page break if the chart
// would not fit in the remaining space.
function drawBarChart(
  doc: jsPDF,
  startY: number,
  opts: {
    title: string;
    items: { label: string; value: number }[];
    color: [number, number, number];
    valueFormatter: (v: number) => string;
  }
): number {
  const marginX = 40;
  const pageW = doc.internal.pageSize.getWidth();
  const pageH = doc.internal.pageSize.getHeight();
  const contentW = pageW - marginX * 2;
  const labelW = 140;
  const valueW = 70;
  const gap = 8;
  const rowH = 16;
  const barH = 9;
  const titleH = 18;

  const items = opts.items.slice(0, 10);
  if (items.length === 0) return startY;

  const chartH = titleH + items.length * rowH + 6;
  let y = startY;
  if (y + chartH > pageH - 40) {
    doc.addPage();
    y = 48;
  }

  doc.setFont("helvetica", "bold");
  doc.setFontSize(11);
  doc.setTextColor(30, 41, 59);
  doc.text(opts.title, marginX, y);
  doc.setFont("helvetica", "normal");
  y += titleH;

  const max = Math.max(...items.map((i) => i.value), 0);
  const barX0 = marginX + labelW;
  const barMaxW = contentW - labelW - valueW - gap;
  const rightX = marginX + contentW;

  for (const it of items) {
    const barLen = max > 0 ? Math.max((it.value / max) * barMaxW, it.value > 0 ? 2 : 0) : 0;

    doc.setFontSize(8);
    doc.setTextColor(71, 85, 105);
    const label = it.label.length > 28 ? `${it.label.slice(0, 27)}…` : it.label;
    doc.text(label, marginX, y + barH - 1);

    doc.setFillColor(opts.color[0], opts.color[1], opts.color[2]);
    if (barLen > 0) doc.roundedRect(barX0, y, barLen, barH, 1.5, 1.5, "F");

    doc.setTextColor(30, 41, 59);
    doc.text(opts.valueFormatter(it.value), rightX, y + barH - 1, { align: "right" });

    y += rowH;
  }

  doc.setTextColor(0);
  return y + 6;
}

// exportUsageReportPdf builds a data-driven (text-selectable) PDF from a usage
// report. Not a screenshot — each section is rendered as a jspdf-autotable so
// the output stays crisp and copy-pasteable. Charts are drawn natively with
// jsPDF primitives (see drawBarChart) rather than captured from the DOM.
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

  // Most used profiles — chart then aggregate table
  let chartY = drawBarChart(doc, (doc as any).lastAutoTable.finalY + 20, {
    title: "Most Used Profiles",
    items: report.profiles.slice(0, 10).map((p) => ({ label: p.profile, value: p.cluster_count })),
    color: [36, 99, 235],
    valueFormatter: (v) => String(v),
  });
  autoTable(doc, {
    startY: chartY,
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

  // Most active users — chart then table
  chartY = drawBarChart(doc, (doc as any).lastAutoTable.finalY + 20, {
    title: "Most Active Users",
    items: report.users.slice(0, 10).map((u) => ({ label: u.owner, value: u.estimated_cost })),
    color: [16, 183, 127],
    valueFormatter: (v) => formatCurrency(v),
  });
  autoTable(doc, {
    startY: chartY,
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

  // Most used OpenShift versions — chart then table
  chartY = drawBarChart(doc, (doc as any).lastAutoTable.finalY + 20, {
    title: "Most Used OpenShift Versions",
    items: report.versions.slice(0, 10).map((v) => ({ label: v.version, value: v.cluster_count })),
    color: [251, 189, 35],
    valueFormatter: (v) => String(v),
  });
  autoTable(doc, {
    startY: chartY,
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

  // Most used add-ons — chart then table
  chartY = drawBarChart(doc, (doc as any).lastAutoTable.finalY + 20, {
    title: "Most Used Add-ons",
    items: report.addons.slice(0, 10).map((a) => ({ label: a.addon, value: a.cluster_count })),
    color: [124, 59, 237],
    valueFormatter: (v) => String(v),
  });
  autoTable(doc, {
    startY: chartY,
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
