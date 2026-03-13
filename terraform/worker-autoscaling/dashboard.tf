# CloudWatch Dashboard for Worker Auto-Scaling Metrics

resource "aws_cloudwatch_dashboard" "worker_metrics" {
  dashboard_name = "${var.project_name}-worker-metrics"

  dashboard_body = jsonencode({
    widgets = [
      # Pending Jobs Metric
      {
        type = "metric"
        properties = {
          metrics = [
            ["OCPCTL", "PendingJobs", { stat = "Average", label = "Pending Jobs" }],
            ["...", { stat = "Maximum", label = "Peak Pending" }]
          ]
          period = 60
          stat   = "Average"
          region = var.aws_region
          title  = "Job Queue Depth"
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 12
        height = 6
        x      = 0
        y      = 0
      },

      # Active Workers Count
      {
        type = "metric"
        properties = {
          metrics = [
            [{ expression = "SEARCH('{OCPCTL,WorkerID} MetricName=\"WorkerActive\"', 'Sum', 60)", id = "e1", label = "Active Workers" }]
          ]
          period = 60
          stat   = "Sum"
          region = var.aws_region
          title  = "Active Worker Instances"
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 12
        height = 6
        x      = 12
        y      = 0
      },

      # ASG Instance Count
      {
        type = "metric"
        properties = {
          metrics = [
            ["AWS/AutoScaling", "GroupDesiredCapacity", "AutoScalingGroupName", aws_autoscaling_group.worker.name, { stat = "Average", label = "Desired Capacity" }],
            [".", "GroupInServiceInstances", ".", ".", { stat = "Average", label = "In Service" }],
            [".", "GroupMinSize", ".", ".", { stat = "Average", label = "Min Size" }],
            [".", "GroupMaxSize", ".", ".", { stat = "Average", label = "Max Size" }]
          ]
          period = 60
          stat   = "Average"
          region = var.aws_region
          title  = "Auto Scaling Group Size"
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 12
        height = 6
        x      = 0
        y      = 6
      },

      # Jobs per Worker (Target Metric)
      {
        type = "metric"
        properties = {
          metrics = [
            [
              {
                expression = "m1 / MAX([FILL(m2, 1)])"
                label      = "Jobs per Worker"
                id         = "e1"
              }
            ],
            [
              "OCPCTL",
              "PendingJobs",
              "AutoScalingGroupName",
              aws_autoscaling_group.worker.name,
              {
                id      = "m1"
                visible = false
                stat    = "Average"
              }
            ],
            [
              {
                expression = "SEARCH('{OCPCTL,WorkerID} MetricName=\"WorkerActive\"', 'Sum', 60)"
                id         = "m2"
                visible    = false
                label      = ""
              }
            ]
          ]
          period = 60
          region = var.aws_region
          title  = "Jobs per Worker (Target: ${var.pending_jobs_per_worker})"
          annotations = {
            horizontal = [
              {
                label = "Target: ${var.pending_jobs_per_worker}"
                value = var.pending_jobs_per_worker
                fill  = "above"
                color = "#ff7f0e"
              }
            ]
          }
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 12
        height = 6
        x      = 12
        y      = 6
      },

      # Job Processing Metrics
      {
        type = "metric"
        properties = {
          metrics = [
            ["OCPCTL", "JobStarted", { stat = "Sum", label = "Jobs Started" }],
            [".", "JobSucceeded", { stat = "Sum", label = "Jobs Succeeded" }],
            [".", "JobFailed", { stat = "Sum", label = "Jobs Failed" }],
            [".", "JobRetried", { stat = "Sum", label = "Jobs Retried" }]
          ]
          period = 300
          stat   = "Sum"
          region = var.aws_region
          title  = "Job Throughput (5 min)"
          yAxis = {
            left = {
              min = 0
            }
          }
        }
        width  = 24
        height = 6
        x      = 0
        y      = 12
      },

      # Alarm Status
      {
        type = "alarm"
        properties = {
          title  = "CloudWatch Alarms"
          alarms = [
            aws_cloudwatch_metric_alarm.high_pending_jobs.arn,
            aws_cloudwatch_metric_alarm.no_active_workers.arn
          ]
        }
        width  = 24
        height = 4
        x      = 0
        y      = 18
      }
    ]
  })
}

# Output dashboard URL
output "dashboard_url" {
  description = "URL to CloudWatch dashboard"
  value       = "https://console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards:name=${aws_cloudwatch_dashboard.worker_metrics.dashboard_name}"
}
